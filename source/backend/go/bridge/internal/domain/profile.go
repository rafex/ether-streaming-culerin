package domain

import "fmt"

// QualityProfile is what the bridge asks every participant to publish
// (signaling v2 `welcome.profile` / `profile`). Ranges match the webrelay's
// parser: a value outside them makes the client reject the message.
type QualityProfile struct {
	Width           int `json:"width"`
	Height          int `json:"height"`
	Framerate       int `json:"framerate"`
	VideoBitrate    int `json:"videoBitrate"`
	AudioBitrate    int `json:"audioBitrate"`
	ScreenFramerate int `json:"screenFramerate"`
	ScreenBitrate   int `json:"screenBitrate"`
}

// DefaultProfile keeps a 720p camera at a bitrate that leaves room for
// several participants on a home uplink.
var DefaultProfile = QualityProfile{
	Width:           1280,
	Height:          720,
	Framerate:       30,
	VideoBitrate:    1_500_000,
	AudioBitrate:    96_000,
	ScreenFramerate: 15,
	ScreenBitrate:   2_000_000,
}

// Validate checks every field against the contract's ranges.
func (p QualityProfile) Validate() error {
	checks := []struct {
		name     string
		value    int
		min, max int
	}{
		{"width", p.Width, 160, 3840},
		{"height", p.Height, 90, 2160},
		{"framerate", p.Framerate, 1, 60},
		{"videoBitrate", p.VideoBitrate, 100_000, 20_000_000},
		{"audioBitrate", p.AudioBitrate, 32_000, 320_000},
		{"screenFramerate", p.ScreenFramerate, 1, 60},
		{"screenBitrate", p.ScreenBitrate, 100_000, 20_000_000},
	}
	for _, c := range checks {
		if c.value < c.min || c.value > c.max {
			return fmt.Errorf("profile.%s=%d outside [%d, %d]", c.name, c.value, c.min, c.max)
		}
	}
	return nil
}

// OutputSettings is the encoding of the composed broadcast (server side; the
// webrelay never sees it).
type OutputSettings struct {
	Width        int `json:"width"`
	Height       int `json:"height"`
	Framerate    int `json:"framerate"`
	VideoBitrate int `json:"videoBitrate"`
	AudioBitrate int `json:"audioBitrate"`
}

// DefaultOutput follows the usual 720p30 live recommendations (2 s GOP is
// set by the encoder adapter).
var DefaultOutput = OutputSettings{
	Width:        1280,
	Height:       720,
	Framerate:    30,
	VideoBitrate: 3_500_000,
	AudioBitrate: 160_000,
}

// Validate keeps the output within what x264 and the RTMP ingest accept;
// sizes must be even for yuv420p.
func (o OutputSettings) Validate() error {
	switch {
	case o.Width < 320 || o.Width > 3840 || o.Width%2 != 0:
		return fmt.Errorf("output width %d must be even and in [320, 3840]", o.Width)
	case o.Height < 180 || o.Height > 2160 || o.Height%2 != 0:
		return fmt.Errorf("output height %d must be even and in [180, 2160]", o.Height)
	case o.Framerate < 10 || o.Framerate > 60:
		return fmt.Errorf("output framerate %d outside [10, 60]", o.Framerate)
	case o.VideoBitrate < 300_000 || o.VideoBitrate > 50_000_000:
		return fmt.Errorf("output videoBitrate %d outside [300000, 50000000]", o.VideoBitrate)
	case o.AudioBitrate < 64_000 || o.AudioBitrate > 320_000:
		return fmt.Errorf("output audioBitrate %d outside [64000, 320000]", o.AudioBitrate)
	}
	return nil
}
