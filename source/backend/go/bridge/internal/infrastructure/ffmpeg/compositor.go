// Package ffmpeg implements ports.Compositor with FFmpeg: a compositor
// process per layout (RTP inputs → overlay/amix → x264/AAC → FLV) whose
// output is spliced into a long-lived publisher process (tee to every
// RTMP(S) destination).
package ffmpeg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// Config locates FFmpeg and its resources.
type Config struct {
	Binary     string
	FontFile   string // empty: autodetect; none found: no text
	WorkDir    string
	RTPPortMin int
	RTPPortMax int
	// RecordDir, when set, also writes each broadcast to <dir>/<room>-<time>.flv.
	RecordDir string
}

// Compositor implements ports.Compositor.
type Compositor struct {
	cfg   Config
	tap   ports.MediaTap
	log   *slog.Logger
	ports *portPool

	mu    sync.Mutex
	rooms map[domain.RoomID]*pipeline
}

var _ ports.Compositor = (*Compositor)(nil)

// New checks the FFmpeg binary and prepares the work directory.
func New(cfg Config, tap ports.MediaTap, log *slog.Logger) (*Compositor, error) {
	if _, err := exec.LookPath(cfg.Binary); err != nil {
		return nil, fmt.Errorf("ffmpeg not found (%s): %w", cfg.Binary, err)
	}
	if cfg.FontFile == "" {
		cfg.FontFile = findFont()
	}
	if cfg.FontFile == "" {
		log.Warn("no font found: the broadcast will not show names (set BRIDGE_FONT_FILE)")
	}
	dir, err := os.MkdirTemp(cfg.WorkDir, "ether-bridge-")
	if err != nil {
		return nil, fmt.Errorf("work dir: %w", err)
	}
	cfg.WorkDir = dir
	return &Compositor{cfg: cfg, tap: tap, log: log, ports: newPortPool(cfg.RTPPortMin, cfg.RTPPortMax),
		rooms: map[domain.RoomID]*pipeline{}}, nil
}

// Close stops every pipeline and removes the work directory.
func (c *Compositor) Close() {
	c.mu.Lock()
	rooms := make([]*pipeline, 0, len(c.rooms))
	for _, p := range c.rooms {
		rooms = append(rooms, p)
	}
	c.rooms = map[domain.RoomID]*pipeline{}
	c.mu.Unlock()
	for _, p := range rooms {
		p.stop()
	}
	_ = os.RemoveAll(c.cfg.WorkDir)
}

// Apply implements ports.Compositor.
func (c *Compositor) Apply(room domain.RoomID, plan domain.CompositionPlan, outputs []ports.Output) error {
	c.mu.Lock()
	p := c.rooms[room]
	if p == nil {
		p = &pipeline{c: c, room: room, dir: filepath.Join(c.cfg.WorkDir, string(room))}
		c.rooms[room] = p
	}
	c.mu.Unlock()
	return p.apply(plan, outputs)
}

// Stop implements ports.Compositor.
func (c *Compositor) Stop(room domain.RoomID) {
	c.mu.Lock()
	p := c.rooms[room]
	delete(c.rooms, room)
	c.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

// Status implements ports.Compositor.
func (c *Compositor) Status(room domain.RoomID) ports.BroadcastStatus {
	c.mu.Lock()
	p := c.rooms[room]
	c.mu.Unlock()
	if p == nil {
		return ports.BroadcastStatus{}
	}
	return p.status()
}

// pipeline is one room's broadcast.
type pipeline struct {
	c    *Compositor
	room domain.RoomID
	dir  string

	mu       sync.Mutex
	stopped  bool
	plan     domain.CompositionPlan
	outputs  []ports.Output
	publish  *process
	splice   *splicer
	runs     map[int]*process // compositor runs still alive, by generation
	gen      int              // latest generation started
	onAir    int              // generation being published
	since    time.Time
	restarts int
	lastErr  string
}

// startupTimeout bounds how long a new run may take to produce its first
// frame; the previous run stays on air meanwhile.
const startupTimeout = 15 * time.Second

// keyframeRetry is how often a starting run re-requests keyframes from its
// publishers until it is on air.
const keyframeRetry = 100 * time.Millisecond

func (p *pipeline) apply(plan domain.CompositionPlan, outputs []ports.Output) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return errors.New("pipeline stopped")
	}
	if p.publish == nil || !slices.Equal(p.outputs, outputs) {
		// New destinations: a fresh stream from the start.
		p.killLocked()
		if err := p.startPublishLocked(outputs); err != nil {
			return err
		}
		p.outputs = slices.Clone(outputs)
		p.since = time.Now()
	} else if len(p.runs) > 0 && p.plan.Signature() == plan.Signature() {
		return nil
	}
	p.plan = plan
	return p.startComposeLocked()
}

func (p *pipeline) startPublishLocked(outputs []ports.Output) error {
	record := ""
	if dir := p.c.cfg.RecordDir; dir != "" {
		record = filepath.Join(dir, fmt.Sprintf("%s-%s.flv", p.room, time.Now().UTC().Format("20060102T150405Z")))
	}
	args, err := publishArgs(outputs, record)
	if err != nil {
		return err
	}
	proc, err := startProcess(p.c.cfg.Binary, args, true, p.c.log.With("room", p.room, "process", "publish"),
		func(text string) string { return redact(text, outputs) })
	if err != nil {
		return err
	}
	p.publish = proc
	p.splice = newSplicer(proc.stdin, time.Now)
	p.runs = map[int]*process{}
	go func() {
		err := proc.wait()
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.stopped || p.publish != proc {
			return
		}
		// Every destination failed (or FFmpeg died): restart the whole stream.
		p.lastErr = describeExit("publisher", err, proc.lastLines()) // lines are already redacted
		p.restarts++
		p.c.log.Warn("publisher exited; restarting", "room", p.room, "err", p.lastErr)
		p.killLocked()
		time.AfterFunc(2*time.Second, p.restart)
	}()
	return nil
}

func (p *pipeline) restart() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.publish != nil {
		return
	}
	if err := p.startPublishLocked(p.outputs); err != nil {
		p.lastErr = err.Error()
		return
	}
	if err := p.startComposeLocked(); err != nil {
		p.lastErr = err.Error()
	}
}

// startComposeLocked starts a compositor run for p.plan. The run on air
// keeps publishing until the new one produces its first frame.
func (p *pipeline) startComposeLocked() error {
	p.gen++
	gen := p.gen
	run := &composeRun{dir: filepath.Join(p.dir, fmt.Sprintf("run-%d", gen)), done: make(chan struct{})}
	if err := os.MkdirAll(run.dir, 0o700); err != nil {
		return err
	}

	// One RTP input per distinct source the plan needs.
	var refs []domain.SourceRef
	for _, ref := range append(p.plan.VideoSources(), p.plan.Audio...) {
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	var inputs []rtpInput
	for _, ref := range refs {
		in, err := run.addInput(p.c, p.room, ref, len(inputs))
		if err != nil {
			p.c.log.Info("source unavailable for the broadcast", "room", p.room, "source", ref.String(), "err", err)
			continue
		}
		inputs = append(inputs, in)
	}
	args, err := composeArgs(graphParams{plan: p.plan, inputs: inputs, font: p.c.cfg.FontFile, text: run.text})
	if err != nil {
		run.close(p.c)
		return err
	}
	proc, err := startProcess(p.c.cfg.Binary, args, false, p.c.log.With("room", p.room, "process", "compose", "run", gen),
		func(text string) string { return text })
	if err != nil {
		run.close(p.c)
		return err
	}
	proc.onKill = func() { run.close(p.c) }
	p.runs[gen] = proc
	splice := p.splice

	go func() {
		err := splice.feed(gen, proc.stdout, func() { p.takeOver(gen) })
		if err != nil && !errors.Is(err, errSuperseded) {
			p.c.log.Log(context.Background(), feedFailureLevel(err), "splicing compositor output failed", "room", p.room, "err", err)
		}
		proc.kill()
		exitErr := proc.wait()
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.runs, gen)
		if p.stopped || p.splice != splice || errors.Is(err, errSuperseded) || gen < p.onAir {
			return
		}
		// The run on air died, or a pending run failed before its first frame.
		p.lastErr = describeExit("compositor", exitErr, proc.lastLines())
		p.restarts++
		p.c.log.Warn("compositor exited", "room", p.room, "run", gen, "err", p.lastErr)
		time.AfterFunc(time.Second, func() { p.retry(gen) })
	}()
	time.AfterFunc(startupTimeout, func() {
		p.mu.Lock()
		stalled := !p.stopped && p.onAir < gen && p.runs[gen] == proc
		p.mu.Unlock()
		if stalled {
			p.c.log.Warn("compositor run produced nothing in time; retrying", "room", p.room, "run", gen)
			proc.kill()
		}
	})
	go p.requestKeyframes(run, gen, inputs)
	p.c.log.Info("compositor starting", "room", p.room, "run", gen, "tiles", len(p.plan.Tiles), "inputs", len(inputs))
	return nil
}

// feedFailureLevel: reading a run's output fails with os.ErrClosed when WE closed its pipe
// (the run was replaced or the broadcast stopped). That is the normal end of a run, not a
// problem; anything else (a broken stream, a bad header) still deserves a warning. A run that
// really died is reported separately by the "compositor exited" path.
func feedFailureLevel(err error) slog.Level {
	if errors.Is(err, os.ErrClosed) {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

// takeOver is called when run gen publishes its first frame: older runs go.
func (p *pipeline) takeOver(gen int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onAir = gen
	for g, proc := range p.runs {
		if g < gen {
			proc.kill()
		}
	}
	p.c.log.Info("compositor on air", "room", p.room, "run", gen)
}

// retry starts a new run if gen is still the latest one.
func (p *pipeline) retry(gen int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped && p.publish != nil && p.gen == gen {
		if err := p.startComposeLocked(); err != nil {
			p.lastErr = err.Error()
		}
	}
}

// requestKeyframes asks the publishers for keyframes right away and then
// every keyframeRetry until the run goes on air: FFmpeg opens its inputs one
// by one (each socket only exists once opened, so the keyframe it needs must
// arrive after that) and each decoder starts on a keyframe. On air means every
// input decodes (the overlay and mix wait for all of them). The shorter the
// retry, the less each sequentially opened video input waits.
func (p *pipeline) requestKeyframes(run *composeRun, gen int, inputs []rtpInput) {
	ticker := time.NewTicker(keyframeRetry)
	defer ticker.Stop()
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		done := p.stopped || p.onAir >= gen
		p.mu.Unlock()
		if done {
			return
		}
		for _, in := range inputs {
			p.c.tap.RequestKeyframe(p.room, in.ref)
		}
		select {
		case <-run.done:
			return
		case <-ticker.C:
		}
	}
}

func (p *pipeline) killLocked() {
	for _, proc := range p.runs {
		proc.kill()
	}
	p.runs = map[int]*process{}
	if p.publish != nil {
		go p.publish.finish()
		p.publish = nil
	}
	p.splice = nil
}

// stop ends the broadcast and waits for the publisher to close its sessions.
func (p *pipeline) stop() {
	p.mu.Lock()
	p.stopped = true
	publisher := p.publish
	p.publish = nil
	p.killLocked()
	p.mu.Unlock()
	if publisher != nil {
		publisher.finish()
	}
	_ = os.RemoveAll(p.dir)
}

func (p *pipeline) status() ports.BroadcastStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return ports.BroadcastStatus{Live: !p.stopped && p.publish != nil, Since: p.since, Restarts: p.restarts, LastError: p.lastErr}
}

// composeRun owns the per-run resources: SDP files, UDP sockets, taps.
type composeRun struct {
	dir   string
	done  chan struct{} // closed when the run's resources are released
	mu    sync.Mutex
	stops []func()
	conns []*net.UDPConn
	ports []int
	once  sync.Once
}

func (r *composeRun) addInput(c *Compositor, room domain.RoomID, ref domain.SourceRef, n int) (rtpInput, error) {
	port, err := c.ports.acquire()
	if err != nil {
		return rtpInput{}, err
	}
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		c.ports.release(port)
		return rtpInput{}, err
	}
	codec, stop, err := c.tap.Tap(room, ref, func(packet []byte) { _, _ = conn.Write(packet) })
	if err != nil {
		_ = conn.Close()
		c.ports.release(port)
		return rtpInput{}, err
	}
	path := filepath.Join(r.dir, fmt.Sprintf("input-%d.sdp", n))
	if err := os.WriteFile(path, []byte(sdp(port, codec)), 0o600); err != nil {
		stop()
		_ = conn.Close()
		c.ports.release(port)
		return rtpInput{}, err
	}
	r.mu.Lock()
	r.stops = append(r.stops, stop)
	r.conns = append(r.conns, conn)
	r.ports = append(r.ports, port)
	r.mu.Unlock()
	return rtpInput{ref: ref, sdpPath: path}, nil
}

func (r *composeRun) text(key, content string) string {
	path := filepath.Join(r.dir, key+".txt")
	// Written as it is: drawtext runs with expansion=none (see graph.go), so nothing in
	// a participant's name is interpreted.
	_ = os.WriteFile(path, []byte(content), 0o600)
	return path
}

func (r *composeRun) close(c *Compositor) {
	r.once.Do(func() {
		close(r.done)
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, stop := range r.stops {
			stop()
		}
		for _, conn := range r.conns {
			_ = conn.Close()
		}
		for _, port := range r.ports {
			c.ports.release(port)
		}
		_ = os.RemoveAll(r.dir)
	})
}

// process is a supervised FFmpeg child.
type process struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdin  io.WriteCloser
	stdout io.ReadCloser
	done   chan struct{}
	err    error
	onKill func()

	mu    sync.Mutex
	lines []string
}

// startProcess runs FFmpeg; clean scrubs secrets from what is logged.
func startProcess(binary string, args []string, withStdin bool, log *slog.Logger, clean func(string) string) (*process, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = 3 * time.Second
	p := &process{cmd: cmd, cancel: cancel, done: make(chan struct{})}
	var err error
	if withStdin {
		if p.stdin, err = cmd.StdinPipe(); err != nil {
			cancel()
			return nil, err
		}
	} else if p.stdout, err = cmd.StdoutPipe(); err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	log.Debug("ffmpeg", "args", clean(strings.Join(args, " ")))
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := clean(scanner.Text())
			log.Debug("ffmpeg", "line", line)
			p.mu.Lock()
			p.lines = append(p.lines, line)
			if len(p.lines) > 20 {
				p.lines = p.lines[1:]
			}
			p.mu.Unlock()
		}
	}()
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *process) wait() error {
	<-p.done
	return p.err
}

func (p *process) kill() {
	p.cancel()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.onKill != nil {
		go func() {
			<-p.done
			p.onKill()
		}()
	}
}

// finish ends a publisher cleanly: EOF on stdin lets FFmpeg flush and close
// the RTMP sessions (and any recording); it is killed if it lingers.
func (p *process) finish() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		p.kill()
	}
}

func (p *process) lastLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.lines)
}

func describeExit(what string, err error, lines []string) string {
	msg := what + " exited"
	if err != nil {
		msg += ": " + err.Error()
	}
	if len(lines) > 0 {
		msg += " — " + lines[len(lines)-1]
	}
	return msg
}

// redact hides stream keys (the last path segment of each output URL) in
// text that may reach logs, the API or the console (CONV-0002).
func redact(text string, outputs []ports.Output) string {
	for _, o := range outputs {
		if i := strings.LastIndex(o.URL, "/"); i > len("rtmp://") {
			text = strings.ReplaceAll(text, o.URL, o.URL[:i+1]+"••••")
			if key := o.URL[i+1:]; len(key) >= 4 {
				text = strings.ReplaceAll(text, key, "••••")
			}
		}
	}
	return text
}

func findFont() string {
	for _, candidate := range []string{
		"/usr/share/fonts/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/Library/Fonts/Arial.ttf",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// portPool hands out even UDP ports (FFmpeg also binds port+1 for RTCP).
type portPool struct {
	mu       sync.Mutex
	min, max int
	next     int
	used     map[int]bool
}

func newPortPool(lo, hi int) *portPool {
	lo += lo % 2
	return &portPool{min: lo, max: hi, next: lo, used: map[int]bool{}}
}

func (pp *portPool) acquire() (int, error) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	for range (pp.max - pp.min) / 2 {
		port := pp.next
		pp.next += 2
		if pp.next+1 > pp.max {
			pp.next = pp.min
		}
		if pp.used[port] || !udpFree(port) || !udpFree(port+1) {
			continue
		}
		pp.used[port] = true
		return port, nil
	}
	return 0, errors.New("no free RTP port for the compositor")
}

func (pp *portPool) release(port int) {
	pp.mu.Lock()
	delete(pp.used, port)
	pp.mu.Unlock()
}

func udpFree(port int) bool {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
