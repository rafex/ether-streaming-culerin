package ffmpeg

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Variables, not constants, so the tests can run the watch in seconds instead of minutes.
var (
	healthEvery   = 5 * time.Second  // how often the pipeline looks at its own meter
	healthSummary = 30 * time.Second // a healthy broadcast reports this often
)

// slowWrite: a single write to the publisher that took this long means FFmpeg stopped reading
// (its RTMP socket is backed up). It comes well before the connection finally breaks.
const slowWrite = time.Second

// healthMeter counts what the splicer hands to the publisher and how long that takes. Everything
// is read by the pipeline's reporter; it never blocks the media path (one short mutex).
type healthMeter struct {
	now func() time.Time

	mu         sync.Mutex
	started    time.Time
	bytes      uint64
	video      uint64
	audio      uint64
	lastTag    time.Time
	maxWrite   time.Duration
	slowWrites int
	last       healthSample // totals at the previous sample
}

func newHealthMeter(now func() time.Time) *healthMeter {
	return &healthMeter{now: now, started: now()}
}

// tag records one media tag written for the broadcast (nil-safe).
func (m *healthMeter) tag(kind byte) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if kind == flvTagVideo {
		m.video++
	} else {
		m.audio++
	}
	m.lastTag = m.now()
}

// writer wraps the publisher's stdin to measure how long writes take and how much goes through.
func (m *healthMeter) writer(w io.Writer) io.Writer { return &meteredWriter{w: w, m: m} }

type meteredWriter struct {
	w io.Writer
	m *healthMeter
}

func (mw *meteredWriter) Write(p []byte) (int, error) {
	start := mw.m.now()
	n, err := mw.w.Write(p)
	took := mw.m.now().Sub(start)
	mw.m.mu.Lock()
	mw.m.bytes += uint64(n)
	if took > mw.m.maxWrite {
		mw.m.maxWrite = took
	}
	if took >= slowWrite {
		mw.m.slowWrites++
	}
	mw.m.mu.Unlock()
	return n, err
}

// healthSample is what happened since the previous sample.
type healthSample struct {
	bytes, video, audio uint64
	interval            time.Duration
	tagAge              time.Duration // since the last tag was written (-1: none yet)
	maxWrite            time.Duration
	slowWrites          int
	totalBytes          uint64
	uptime              time.Duration
}

func (s healthSample) kbps() float64 {
	if s.interval <= 0 {
		return 0
	}
	return float64(s.bytes) * 8 / 1000 / s.interval.Seconds()
}

func (s healthSample) videoFPS() float64 {
	if s.interval <= 0 {
		return 0
	}
	return float64(s.video) / s.interval.Seconds()
}

// sample returns the activity since the last call and resets the interval extremes.
func (m *healthMeter) sample() healthSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	prevAt := m.last.uptime
	s := healthSample{
		bytes:      m.bytes - m.last.totalBytes,
		video:      m.video - m.last.video,
		audio:      m.audio - m.last.audio,
		interval:   now.Sub(m.started) - prevAt,
		tagAge:     -1,
		maxWrite:   m.maxWrite,
		slowWrites: m.slowWrites,
		totalBytes: m.bytes,
		uptime:     now.Sub(m.started),
	}
	if !m.lastTag.IsZero() {
		s.tagAge = now.Sub(m.lastTag)
	}
	// Keep the running totals in `last` so the next sample is a delta.
	m.last = healthSample{totalBytes: m.bytes, video: m.video, audio: m.audio, uptime: s.uptime}
	m.maxWrite, m.slowWrites = 0, 0
	return s
}

// problems names what looks wrong in a sample; empty when healthy. onAir: a run is being published
// (before that, silence is expected).
func (s healthSample) problems(onAir bool) []string {
	if !onAir {
		return nil
	}
	var out []string
	switch {
	case s.tagAge < 0:
		out = append(out, "no media written yet")
	case s.tagAge >= 3*time.Second:
		out = append(out, fmt.Sprintf("output stalled %.1fs", s.tagAge.Seconds()))
	}
	if s.slowWrites > 0 || s.maxWrite >= slowWrite {
		out = append(out, fmt.Sprintf("publisher not reading: write took %dms (network or platform backpressure)", s.maxWrite.Milliseconds()))
	}
	if s.interval >= healthEvery/2 && s.bytes == 0 {
		out = append(out, "0 bytes sent in the interval")
	}
	return out
}

// totals is the cumulative bytes written and the time since the last tag (-1: none yet), without
// touching the interval counters (used when the publisher dies).
func (m *healthMeter) totals() (uint64, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	age := time.Duration(-1)
	if !m.lastTag.IsZero() {
		age = m.now().Sub(m.lastTag)
	}
	return m.bytes, age
}
