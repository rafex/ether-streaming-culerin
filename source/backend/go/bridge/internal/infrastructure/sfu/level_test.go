package sfu

import (
	"testing"
	"time"
)

func TestParseAudioLevel(t *testing.T) {
	voice, level, ok := parseAudioLevel([]byte{0x80 | 23})
	if !ok || !voice || level != 23 {
		t.Fatalf("got voice=%v level=%d ok=%v", voice, level, ok)
	}
	voice, level, ok = parseAudioLevel([]byte{127})
	if !ok || voice || level != 127 {
		t.Fatalf("silence: got voice=%v level=%d ok=%v", voice, level, ok)
	}
	if _, _, ok := parseAudioLevel(nil); ok {
		t.Fatal("an empty extension must not parse")
	}
}

func TestIsSpeech(t *testing.T) {
	cases := []struct {
		voiceBit bool
		level    int
		want     bool
	}{
		{false, 20, true},   // normal speech
		{false, 45, true},   // the edge
		{false, 46, false},  // room noise
		{true, 55, true},    // quiet voice the sender flagged
		{true, 56, false},   // flagged but too quiet to matter
		{false, 127, false}, // digital silence
	}
	for _, c := range cases {
		if got := isSpeech(c.voiceBit, c.level); got != c.want {
			t.Errorf("isSpeech(%v,%d)=%v, want %v", c.voiceBit, c.level, got, c.want)
		}
	}
}

func feed(m *activityMeter, from time.Time, d time.Duration, speech bool) time.Time {
	t := from
	for ; t.Sub(from) < d; t = t.Add(20 * time.Millisecond) {
		m.observe(speech, t)
	}
	return t
}

func TestActivityRisesWithSpeechAndFallsInSilence(t *testing.T) {
	var m activityMeter
	start := time.Unix(1000, 0)
	now := feed(&m, start, 2*time.Second, true)
	if v := m.read(now); v < 0.9 {
		t.Fatalf("after 2 s of speech activity = %.2f, want >= 0.9", v)
	}
	now = feed(&m, now, 2*time.Second, false)
	if v := m.read(now); v > 0.05 {
		t.Fatalf("after 2 s of silence activity = %.2f, want <= 0.05", v)
	}
}

func TestActivityDecaysWhenPacketsStop(t *testing.T) {
	var m activityMeter
	now := feed(&m, time.Unix(1000, 0), 2*time.Second, true)
	if v := m.read(now.Add(3 * time.Second)); v > 0.05 {
		t.Fatalf("3 s without packets activity = %.2f, want <= 0.05 (a muted mic is not talking)", v)
	}
}

func TestActivityIsIndependentOfThePacketRate(t *testing.T) {
	// Two packets per 20 ms (a duplicate stream) must not make speech look louder than one.
	var a, b activityMeter
	start := time.Unix(1000, 0)
	for i := 0; i < 50; i++ {
		ts := start.Add(time.Duration(i) * 20 * time.Millisecond)
		a.observe(true, ts)
		b.observe(true, ts)
	}
	end := start.Add(time.Second)
	if va, vb := a.read(end), b.read(end); va != vb {
		t.Fatalf("same stream, different readings: %.4f vs %.4f", va, vb)
	}
}
