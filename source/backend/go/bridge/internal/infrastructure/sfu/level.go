package sfu

import (
	"math"
	"sync"
	"time"
)

// audioLevelURI is the RTP header extension (RFC 6464) in which a browser reports how loud the
// packet's audio is: one byte, a voice-activity bit and the level as -dBov (0 loudest, 127 silence).
const audioLevelURI = "urn:ietf:params:rtp-hdrext:ssrc-audio-level"

const (
	// voiceLevel: a packet at least this loud (-dBov value at or below) is speech. The voice-activity
	// bit alone is not trusted (not every browser fills it), the level is.
	voiceLevel = 45
	// voiceBitLevel: with the sender's own voice bit set a slightly quieter packet counts too.
	voiceBitLevel = 55
	// activityWindow is the time constant of the activity average: ~0.5 s of speech reads as 0.63.
	activityWindow = 500 * time.Millisecond
)

// parseAudioLevel reads the extension payload of RFC 6464.
func parseAudioLevel(payload []byte) (voiceBit bool, level int, ok bool) {
	if len(payload) < 1 {
		return false, 0, false
	}
	return payload[0]&0x80 != 0, int(payload[0] & 0x7f), true
}

// isSpeech tells whether a packet with this level carries a voice.
func isSpeech(voiceBit bool, level int) bool {
	return level <= voiceLevel || (voiceBit && level <= voiceBitLevel)
}

// packetStep is the audio each packet stands for (Opus sends 20 ms frames).
const packetStep = 20 * time.Millisecond

// activityMeter is a time-weighted average of "is somebody talking" over the last ~half second,
// fed by packets. Time without packets counts as silence: a muted or gone source decays to zero.
type activityMeter struct {
	mu    sync.Mutex
	value float64   // the average as of `at`
	at    time.Time // when the last packet was heard
}

// observe folds one packet (speech or not) heard at now.
func (m *activityMeter) observe(speech bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.at.IsZero() {
		// Only the part of the gap longer than one packet was silence nobody reported.
		m.value *= decay(now.Sub(m.at) - packetStep)
	}
	target := 0.0
	if speech {
		target = 1
	}
	// The packet represents one step; a fixed step keeps the average independent of the packet rate.
	alpha := 1 - decay(packetStep)
	m.value += (target - m.value) * alpha
	m.at = now
}

// read is the activity at now (0..1).
func (m *activityMeter) read(now time.Time) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.at.IsZero() {
		return 0
	}
	return m.value * decay(now.Sub(m.at)-packetStep)
}

// decay is the fraction of the average left after d of silence (1 for d <= 0).
func decay(d time.Duration) float64 {
	if d <= 0 {
		return 1
	}
	return math.Exp(-float64(d) / float64(activityWindow))
}
