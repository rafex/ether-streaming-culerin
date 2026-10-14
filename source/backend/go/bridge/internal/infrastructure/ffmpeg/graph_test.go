package ffmpeg

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

func plan() domain.CompositionPlan {
	ana := domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}
	beto := domain.SourceRef{Participant: "p-2", Source: domain.SourceCamera}
	return domain.CompositionPlan{
		Output: domain.DefaultOutput, Title: "Demo",
		Tiles: []domain.Tile{
			{Rect: domain.Rect{X: 12, Y: 186, W: 620, H: 348}, Kind: domain.TileVideo, Source: ana, Label: "Ana"},
			{Rect: domain.Rect{X: 648, Y: 186, W: 620, H: 348}, Kind: domain.TileCard, Source: beto, Label: "Beto"},
		},
		Audio: []domain.SourceRef{{Participant: "p-1", Source: domain.SourceMicrophone}},
	}
}

func text(key, _ string) string { return "/w/" + key + ".txt" }

func TestComposeArgs(t *testing.T) {
	inputs := []rtpInput{
		{ref: domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}, sdpPath: "/w/input-0.sdp"},
		{ref: domain.SourceRef{Participant: "p-1", Source: domain.SourceMicrophone}, sdpPath: "/w/input-1.sdp"},
	}
	args, err := composeArgs(graphParams{plan: plan(), inputs: inputs, font: "/f/DejaVuSans.ttf", text: text})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{
		"-copyts", "-use_wallclock_as_timestamps 1", "-i /w/input-0.sdp", "-i /w/input-1.sdp",
		"color=c=0x14161c:s=1280x720:r=30", "realtime",
		"drawbox=x=648:y=186:w=620:h=348", "textfile=/w/card-1.txt", // Beto's card
		"[0:v]fps=30,scale=620:348", "overlay=x=12:y=186", "textfile=/w/label-0.txt", // Ana's tile
		"setparams=range=tv", "setpts=PTS-STARTPTS[vout]",
		"[1:a]aresample=48000", "amix=inputs=2", "anullsrc",
		"-c:v libx264", "-g 60", "-bf 0", "-b:v 3500k", "-c:a aac", "-b:a 160k", "-f flv", "pipe:1",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in\n%s", want, line)
		}
	}
}

func TestComposeArgsWithoutInputsOrFont(t *testing.T) {
	args, err := composeArgs(graphParams{plan: plan(), font: "", text: text})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	if strings.Contains(line, "-i ") || strings.Contains(line, "drawtext") || strings.Count(line, "drawbox") != 2 {
		t.Fatalf("a tile without input must become a card, no text without font:\n%s", line)
	}
	if !strings.Contains(line, "amix=inputs=1") {
		t.Fatalf("silent bed only: %s", line)
	}
	empty := domain.CompositionPlan{Output: domain.DefaultOutput, Title: "Demo"}
	args, _ = composeArgs(graphParams{plan: empty, font: "/f/a.ttf", text: text})
	if line := strings.Join(args, " "); !strings.Contains(line, "/w/title.txt") || !strings.Contains(line, "/w/subtitle.txt") {
		t.Fatalf("empty stage shows the title: %s", line)
	}
}

func TestUnsafePathsAreRejected(t *testing.T) {
	bad := []rtpInput{{ref: domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}, sdpPath: "/w/a:b.sdp"}}
	if _, err := composeArgs(graphParams{plan: plan(), inputs: bad, text: text}); err == nil {
		t.Fatal("unsafe sdp path accepted")
	}
	if _, err := composeArgs(graphParams{plan: plan(), font: "/f/my font.ttf", text: text}); err == nil {
		t.Fatal("unsafe font path accepted")
	}
}

func TestPublishArgs(t *testing.T) {
	args, err := publishArgs([]ports.Output{{ID: "d-1", URL: "rtmps://a/live/key1"}, {ID: "d-2", URL: "rtmp://b/app/key2"}}, "/rec/demo.flv")
	if err != nil {
		t.Fatal(err)
	}
	last := args[len(args)-1]
	want := "[f=flv:onfail=ignore:flvflags=no_duration_filesize]rtmps://a/live/key1|[f=flv:onfail=ignore:flvflags=no_duration_filesize]rtmp://b/app/key2|[f=flv:onfail=ignore]/rec/demo.flv"
	if last != want || !strings.Contains(strings.Join(args, " "), "-c copy -f tee") {
		t.Fatalf("tee = %s", last)
	}
	if _, err := publishArgs(nil, ""); err == nil {
		t.Fatal("no outputs accepted")
	}
	if _, err := publishArgs([]ports.Output{{ID: "x", URL: "rtmp://a/b|c"}}, ""); err == nil {
		t.Fatal("tee injection accepted")
	}
	if _, err := publishArgs([]ports.Output{{ID: "x", URL: "rtmp://a/b"}}, "/rec/a b.flv"); err == nil {
		t.Fatal("unsafe recording path accepted")
	}
}

func TestSDP(t *testing.T) {
	video := sdp(40000, ports.Codec{MimeType: "video/VP8", ClockRate: 90000, PayloadType: 96})
	if !strings.Contains(video, "m=video 40000 RTP/AVP 96\r\na=rtpmap:96 VP8/90000\r\n") || strings.Contains(video, "fmtp") {
		t.Fatalf("video sdp:\n%s", video)
	}
	audio := sdp(40002, ports.Codec{MimeType: "audio/opus", ClockRate: 48000, Channels: 2, PayloadType: 111, Fmtp: "minptime=10"})
	if !strings.Contains(audio, "m=audio 40002 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=fmtp:111 minptime=10\r\n") {
		t.Fatalf("audio sdp:\n%s", audio)
	}
}

// --- splicer with synthetic FLV -------------------------------------------

func flvHeader() []byte {
	return []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
}

func flvTag(kind byte, ts uint32, data []byte) []byte {
	h := []byte{kind, byte(len(data) >> 16), byte(len(data) >> 8), byte(len(data)), 0, 0, 0, 0, 0, 0, 0}
	writeTimestamp(h, ts)
	out := append(h, data...)
	return binary.BigEndian.AppendUint32(out, uint32(11+len(data)))
}

var (
	avcSeq   = []byte{0x17, 0, 0, 0, 0, 1, 2, 3}
	aacSeq   = []byte{0xaf, 0, 0x12, 0x10}
	keyframe = []byte{0x17, 1, 0, 0, 0, 9, 9}
	aacFrame = []byte{0xaf, 1, 7}
)

func run(tags ...[]byte) *bytes.Reader {
	b := flvHeader()
	for _, t := range tags {
		b = append(b, t...)
	}
	return bytes.NewReader(b)
}

type parsedTag struct {
	kind byte
	ts   uint32
}

func parse(t *testing.T, b []byte) []parsedTag {
	t.Helper()
	if !bytes.HasPrefix(b, []byte("FLV")) {
		t.Fatal("missing FLV header")
	}
	var tags []parsedTag
	for p := 13; p < len(b); {
		size := int(b[p+1])<<16 | int(b[p+2])<<8 | int(b[p+3])
		ts := uint32(b[p+4])<<16 | uint32(b[p+5])<<8 | uint32(b[p+6]) | uint32(b[p+7])<<24
		if got := binary.BigEndian.Uint32(b[p+11+size:]); got != uint32(11+size) {
			t.Fatalf("bad PreviousTagSize at %d", p)
		}
		tags = append(tags, parsedTag{b[p], ts})
		p += 15 + size
	}
	return tags
}

func TestSplicerJoinsRunsOnOneTimeline(t *testing.T) {
	clock := time.Unix(100, 0)
	var out bytes.Buffer
	s := newSplicer(&out, func() time.Time { return clock })
	onAir := 0

	first := run(flvTag(flvTagScript, 0, []byte{2}), flvTag(flvTagVideo, 0, avcSeq), flvTag(flvTagAudio, 0, aacSeq),
		flvTag(flvTagVideo, 0, keyframe), flvTag(flvTagAudio, 20, aacFrame), flvTag(flvTagVideo, 33, keyframe))
	if err := s.feed(1, first, func() { onAir++ }); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(5 * time.Second)
	second := run(flvTag(flvTagScript, 0, []byte{2}), flvTag(flvTagVideo, 0, avcSeq), flvTag(flvTagAudio, 0, aacSeq),
		flvTag(flvTagVideo, 0, keyframe), flvTag(flvTagVideo, 33, keyframe))
	if err := s.feed(2, second, func() { onAir++ }); err != nil {
		t.Fatal(err)
	}

	tags := parse(t, out.Bytes())
	kinds := ""
	for _, tag := range tags {
		kinds += string("SAV"[map[byte]int{flvTagScript: 0, flvTagAudio: 1, flvTagVideo: 2}[tag.kind]])
	}
	// One header, one metadata tag, sequence headers once (identical), all frames.
	if kinds != "SVAVAVVV" || onAir != 2 {
		t.Fatalf("tags = %s (onAir %d)", kinds, onAir)
	}
	if tags[6].ts != 5000 || tags[7].ts != 5033 {
		t.Fatalf("second run must continue on the wall clock: %+v", tags)
	}
}

func TestSplicerKeepsChangedSequenceHeadersAndDropsSupersededRuns(t *testing.T) {
	clock := time.Unix(0, 0)
	var out bytes.Buffer
	s := newSplicer(&out, func() time.Time { return clock })
	s.feed(2, run(flvTag(flvTagVideo, 0, avcSeq), flvTag(flvTagVideo, 0, keyframe)), func() {})
	// An older run that ends late is ignored.
	if err := s.feed(1, run(flvTag(flvTagVideo, 0, keyframe)), func() { t.Fatal("old run on air") }); err != errSuperseded {
		t.Fatalf("expected errSuperseded, got %v", err)
	}
	changed := append([]byte(nil), avcSeq...)
	changed[7] = 9
	s.feed(3, run(flvTag(flvTagVideo, 0, changed), flvTag(flvTagVideo, 0, keyframe)), func() {})
	if tags := parse(t, out.Bytes()); len(tags) != 4 || tags[2].ts < tags[1].ts {
		t.Fatalf("tags = %+v", tags)
	}
	if err := s.feed(4, bytes.NewReader([]byte("NOTFLV-------")), func() {}); err == nil {
		t.Fatal("non-FLV input accepted")
	}
	if err := s.feed(5, bytes.NewReader(nil), func() {}); err != nil {
		t.Fatalf("an empty run is not an error: %v", err)
	}
}

func TestRedactHidesStreamKeys(t *testing.T) {
	outputs := []ports.Output{{ID: "d-1", URL: "rtmps://a.rtmp.youtube.com/live2/abcd-efgh-1234"}}
	got := redact("[tee] rtmps://a.rtmp.youtube.com/live2/abcd-efgh-1234: I/O error (key abcd-efgh-1234)", outputs)
	if strings.Contains(got, "abcd-efgh-1234") || !strings.Contains(got, "rtmps://a.rtmp.youtube.com/live2/••••") {
		t.Fatalf("redacted = %s", got)
	}
}

// Audio must keep the RTP media clock: wall-clock stamping of bursty packets made
// aresample (async) insert blocks of silence and starve the live inputs.
func TestAudioInputsUseTheMediaClock(t *testing.T) {
	inputs := []rtpInput{
		{ref: domain.SourceRef{Participant: "p-1", Source: domain.SourceCamera}, sdpPath: "/w/input-0.sdp"},
		{ref: domain.SourceRef{Participant: "p-1", Source: domain.SourceMicrophone}, sdpPath: "/w/input-1.sdp"},
	}
	args, err := composeArgs(graphParams{plan: plan(), inputs: inputs, font: "/f/DejaVuSans.ttf", text: text})
	if err != nil {
		t.Fatal(err)
	}
	var perInput [][]string // options preceding each -i
	var current []string
	for _, a := range args {
		if a == "-i" {
			perInput = append(perInput, current)
			current = nil
			continue
		}
		current = append(current, a)
	}
	if len(perInput) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(perInput))
	}
	has := func(opts []string, flag string) bool {
		for _, o := range opts {
			if o == flag {
				return true
			}
		}
		return false
	}
	if !has(perInput[0], "-use_wallclock_as_timestamps") {
		t.Error("video input must be stamped with the wall clock")
	}
	if has(perInput[1], "-use_wallclock_as_timestamps") {
		t.Error("audio input must keep its RTP media clock")
	}
	line := strings.Join(args, " ")
	if strings.Contains(line, "async=") {
		t.Errorf("aresample async must not be used:\n%s", line)
	}
	if !strings.Contains(line, "[1:a]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,asetpts=N/SR/TB[a1]") {
		t.Errorf("audio chain must count samples:\n%s", line)
	}
}
