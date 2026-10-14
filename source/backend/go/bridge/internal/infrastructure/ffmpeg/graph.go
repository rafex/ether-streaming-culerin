package ffmpeg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

const (
	backgroundColor = "0x14161c"
	cardColor       = "0x2a2e38"
	letterboxColor  = "0x000000"
)

// rtpInput is one SDP-described RTP stream fed by a tap.
type rtpInput struct {
	ref     domain.SourceRef
	sdpPath string
}

// graphParams is everything the compose command depends on.
type graphParams struct {
	plan   domain.CompositionPlan
	inputs []rtpInput // ffmpeg input index = position
	font   string     // empty: no text is drawn
	// text stores a label and returns the file drawtext reads it from
	// (textfile avoids escaping user-provided names in the graph).
	text func(key, content string) string
}

var safePath = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// composeArgs builds the FFmpeg command line of one compositor run.
//
// Timestamps: every RTP input is stamped with the wall clock on arrival
// (-use_wallclock_as_timestamps) and -copyts keeps those absolute values, so
// all participants share one timeline (lip sync per participant, no drift
// between them). The background and the silent audio bed are generated in
// the graph, paced by realtime/arealtime and then stamped with the wall clock
// at the moment they are emitted (RTCTIME), so the master timeline always
// tracks real time. (Anchoring them to the process start instead, RTCSTART +
// n/fps, bakes every stall into a permanent offset: the inputs, stamped with
// the true clock, end up that far ahead of the master, FFmpeg stops reading
// them, the UDP socket buffers fill and packets are dropped.) The final setpts
// rebases the output to zero.
func composeArgs(p graphParams) ([]string, error) {
	out := p.plan.Output
	args := []string{"-hide_banner", "-loglevel", "warning", "-nostdin", "-copyts"}
	index := map[domain.SourceRef]int{}
	for i, in := range p.inputs {
		if !safePath.MatchString(in.sdpPath) {
			return nil, fmt.Errorf("unsafe input path %q", in.sdpPath)
		}
		index[in.ref] = i
		args = append(args,
			"-protocol_whitelist", "file,udp,rtp",
			"-use_wallclock_as_timestamps", "1",
			"-fflags", "+nobuffer",
			"-analyzeduration", "0",
			"-probesize", "32",
			"-reorder_queue_size", "64",
			"-thread_queue_size", "1024",
			"-reinit_filter", "0",
			"-i", in.sdpPath)
	}
	if p.font != "" && !safePath.MatchString(p.font) {
		return nil, fmt.Errorf("unsafe font path %q", p.font)
	}

	var g []string
	fps := strconv.Itoa(out.Framerate)
	// Stamp with the real clock after pacing (see composeArgs).
	wallclock := "RTCTIME/1000000/TB"

	// Background with the name cards drawn on it.
	bg := []string{fmt.Sprintf("color=c=%s:s=%dx%d:r=%s", backgroundColor, out.Width, out.Height, fps),
		"format=yuv420p", "realtime", "setpts=" + wallclock}
	for i, t := range p.plan.Tiles {
		_, hasInput := index[t.Source]
		if t.Kind == domain.TileVideo && hasInput {
			continue
		}
		r := t.Rect
		bg = append(bg, fmt.Sprintf("drawbox=x=%d:y=%d:w=%d:h=%d:color=%s:t=fill", r.X, r.Y, r.W, r.H, cardColor))
		if p.font != "" {
			size := clamp(r.H/7, 18, 64)
			bg = append(bg, drawtext(p.font, p.text(fmt.Sprintf("card-%d", i), t.Label), size,
				fmt.Sprintf("%d+(%d-tw)/2", r.X, r.W), fmt.Sprintf("%d+(%d-th)/2", r.Y, r.H), false))
		}
	}
	if len(p.plan.Tiles) == 0 && p.font != "" {
		bg = append(bg,
			drawtext(p.font, p.text("title", p.plan.Title), clamp(out.Height/14, 20, 72), "(w-tw)/2", "(h-th)/2-th", false),
			drawtext(p.font, p.text("subtitle", "Comenzamos en breve"), clamp(out.Height/26, 14, 40), "(w-tw)/2", "(h+th)/2+th/2", false))
	}
	g = append(g, strings.Join(bg, ",")+"[bg]")

	// Live tiles overlaid on the background.
	current := "bg"
	n := 0
	for _, t := range p.plan.Tiles {
		i, ok := index[t.Source]
		if t.Kind != domain.TileVideo || !ok {
			continue
		}
		r := t.Rect
		g = append(g, fmt.Sprintf("[%d:v]fps=%s,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:-1:-1:color=%s,setsar=1,format=yuv420p[t%d]",
			i, fps, r.W, r.H, r.W, r.H, letterboxColor, n))
		next := fmt.Sprintf("o%d", n)
		g = append(g, fmt.Sprintf("[%s][t%d]overlay=x=%d:y=%d:eof_action=repeat[%s]", current, n, r.X, r.Y, next))
		current = next
		n++
	}

	// Name labels on live tiles, then rebase to zero.
	final := []string{"null"}
	if p.font != "" {
		for i, t := range p.plan.Tiles {
			if _, ok := index[t.Source]; t.Kind != domain.TileVideo || !ok {
				continue
			}
			r := t.Rect
			size := clamp(r.H/16, 14, 32)
			final = append(final, drawtext(p.font, p.text(fmt.Sprintf("label-%d", i), t.Label), size,
				fmt.Sprintf("%d+%d", r.X, size/2+4), fmt.Sprintf("%d+%d-th-%d", r.Y, r.H, size/2+4), true))
		}
	}
	// Fixed colour properties: every run must yield the same H.264 SPS so the
	// splicer can drop the repeated sequence header (a mid-stream SPS change
	// trips some decoders).
	final = append(final, "setparams=range=tv:color_primaries=bt709:color_trc=bt709:colorspace=bt709", "setpts=PTS-STARTPTS")
	g = append(g, fmt.Sprintf("[%s]%s[vout]", current, strings.Join(final, ",")))

	// Audio: a silent bed (so the stream always has audio) mixed with every
	// live microphone / screen audio.
	g = append(g, "anullsrc=r=48000:cl=stereo,arealtime,asetpts="+wallclock+"[a0]")
	mix := []string{"[a0]"}
	for k, ref := range p.plan.Audio {
		i, ok := index[ref]
		if !ok {
			continue
		}
		label := fmt.Sprintf("a%d", k+1)
		g = append(g, fmt.Sprintf("[%d:a]aresample=48000:async=1000,aformat=sample_fmts=fltp:channel_layouts=stereo[%s]", i, label))
		mix = append(mix, "["+label+"]")
	}
	g = append(g, fmt.Sprintf("%samix=inputs=%d:duration=first:dropout_transition=0:normalize=0,asetpts=PTS-STARTPTS[aout]",
		strings.Join(mix, ""), len(mix)))

	kbps := func(bps int) string { return strconv.Itoa(bps/1000) + "k" }
	args = append(args,
		"-filter_complex", strings.Join(g, ";"),
		"-map", "[vout]", "-map", "[aout]",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-pix_fmt", "yuv420p",
		"-color_range", "tv", "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709",
		"-b:v", kbps(out.VideoBitrate), "-maxrate", kbps(out.VideoBitrate), "-bufsize", kbps(2*out.VideoBitrate),
		"-g", strconv.Itoa(2*out.Framerate), "-keyint_min", strconv.Itoa(2*out.Framerate), "-sc_threshold", "0", "-bf", "0",
		"-c:a", "aac", "-b:a", kbps(out.AudioBitrate), "-ar", "48000", "-ac", "2",
		"-fps_mode", "passthrough",
		"-f", "flv", "-flvflags", "no_duration_filesize", "pipe:1")
	return args, nil
}

// publishArgs builds the long-lived publisher: FLV on stdin copied to every
// destination. onfail=ignore keeps the others alive if one ingest fails.
func publishArgs(outputs []ports.Output, record string) ([]string, error) {
	targets := make([]string, 0, len(outputs)+1)
	for _, o := range outputs {
		if strings.ContainsAny(o.URL, "|[]' \t\n") {
			return nil, fmt.Errorf("output %s has characters the tee muxer cannot take", o.ID)
		}
		targets = append(targets, "[f=flv:onfail=ignore:flvflags=no_duration_filesize]"+o.URL)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no outputs")
	}
	if record != "" {
		if !safePath.MatchString(record) {
			return nil, fmt.Errorf("unsafe recording path %q", record)
		}
		targets = append(targets, "[f=flv:onfail=ignore]"+record)
	}
	return []string{"-hide_banner", "-loglevel", "warning",
		"-fflags", "+nobuffer", "-f", "flv", "-i", "pipe:0",
		"-map", "0:v", "-map", "0:a", "-c", "copy",
		"-f", "tee", strings.Join(targets, "|")}, nil
}

func drawtext(font, textfile string, size int, x, y string, boxed bool) string {
	s := fmt.Sprintf("drawtext=fontfile=%s:textfile=%s:fontsize=%d:fontcolor=white:x=%s:y=%s", font, textfile, size, x, y)
	if boxed {
		s += ":box=1:boxcolor=0x000000@0.55:boxborderw=8"
	}
	return s
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }

// sdp describes one RTP input for FFmpeg.
func sdp(port int, c ports.Codec) string {
	media, encoding := "video", strings.TrimPrefix(c.MimeType, "video/")
	if strings.HasPrefix(c.MimeType, "audio/") {
		media, encoding = "audio", strings.TrimPrefix(c.MimeType, "audio/")
	}
	rtpmap := fmt.Sprintf("%s/%d", encoding, c.ClockRate)
	if c.Channels > 0 {
		rtpmap += fmt.Sprintf("/%d", c.Channels)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=ether-bridge\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n")
	fmt.Fprintf(&b, "m=%s %d RTP/AVP %d\r\na=rtpmap:%d %s\r\n", media, port, c.PayloadType, c.PayloadType, rtpmap)
	if c.Fmtp != "" {
		fmt.Fprintf(&b, "a=fmtp:%d %s\r\n", c.PayloadType, c.Fmtp)
	}
	b.WriteString("a=recvonly\r\n")
	return b.String()
}
