package ffmpeg

import (
	"bytes"
	"testing"
	"time"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time          { return c.now }
func (c *testClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// slowWriter takes `delay` of (fake) time per write.
type slowWriter struct {
	clock *testClock
	delay time.Duration
	buf   bytes.Buffer
}

func (w *slowWriter) Write(p []byte) (int, error) {
	w.clock.advance(w.delay)
	return w.buf.Write(p)
}

func TestMeterMeasuresRateTagsAndAge(t *testing.T) {
	clock := &testClock{now: time.Unix(1000, 0)}
	m := newHealthMeter(clock.Now)
	w := m.writer(&slowWriter{clock: clock})
	for range 50 {
		_, _ = w.Write(make([]byte, 1250)) // 50 × 1250 B = 62 500 B
		m.tag(flvTagVideo)
	}
	for range 10 {
		m.tag(flvTagAudio)
	}
	clock.advance(5*time.Second - 0) // the interval is 5 s
	s := m.sample()
	if s.bytes != 62500 || s.video != 50 || s.audio != 10 {
		t.Fatalf("sample = %+v", s)
	}
	if got := s.kbps(); got < 99 || got > 101 { // 62500 B * 8 / 5 s = 100 kbps
		t.Errorf("kbps = %.1f, want 100", got)
	}
	if got := s.videoFPS(); got != 10 {
		t.Errorf("video tags/s = %.1f, want 10", got)
	}
	if s.tagAge != 5*time.Second {
		t.Errorf("tag age = %v, want 5s", s.tagAge)
	}

	// The next sample is a delta, not the running total.
	clock.advance(5 * time.Second)
	s = m.sample()
	if s.bytes != 0 || s.video != 0 || s.tagAge != 10*time.Second {
		t.Errorf("second sample = %+v", s)
	}
}

func TestMeterSeesAPublisherThatStopsReading(t *testing.T) {
	clock := &testClock{now: time.Unix(1000, 0)}
	m := newHealthMeter(clock.Now)
	w := m.writer(&slowWriter{clock: clock, delay: 2500 * time.Millisecond})
	_, _ = w.Write([]byte("x"))
	m.tag(flvTagVideo)
	clock.advance(2500 * time.Millisecond)
	s := m.sample()
	if s.slowWrites != 1 || s.maxWrite < 2*time.Second {
		t.Fatalf("slow write not seen: %+v", s)
	}
	problems := s.problems(true)
	if len(problems) == 0 || !contains(problems, "backpressure") {
		t.Fatalf("problems = %v", problems)
	}
}

func TestProblemsOnlyWhileOnAir(t *testing.T) {
	s := healthSample{tagAge: 20 * time.Second, interval: healthEvery}
	if got := s.problems(false); len(got) != 0 {
		t.Errorf("not on air yet: %v", got)
	}
	got := s.problems(true)
	if !contains(got, "stalled") || !contains(got, "0 bytes") {
		t.Errorf("stalled and silent: %v", got)
	}
	healthy := healthSample{tagAge: 100 * time.Millisecond, bytes: 100000, interval: healthEvery}
	if got := healthy.problems(true); len(got) != 0 {
		t.Errorf("healthy flagged: %v", got)
	}
}

func contains(list []string, part string) bool {
	for _, s := range list {
		if bytes.Contains([]byte(s), []byte(part)) {
			return true
		}
	}
	return false
}
