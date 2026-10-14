package domain

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// DestinationKind is the platform of an RTMP(S) output; it is what the lobby
// shows as a channel chip.
type DestinationKind string

const (
	KindYouTube  DestinationKind = "youtube"
	KindTwitch   DestinationKind = "twitch"
	KindFacebook DestinationKind = "facebook"
	KindCustom   DestinationKind = "custom"
)

// Destination is where the composed broadcast is published. The stream key
// is a secret: it never leaves the bridge except masked.
type Destination struct {
	ID        string
	Name      string
	Kind      DestinationKind
	URL       string
	StreamKey string
	Enabled   bool
}

// Validate checks the ingest URL and infers the kind from the host when it
// is not given.
func (d *Destination) Validate() error {
	d.Name = strings.TrimSpace(d.Name)
	d.URL = strings.TrimRight(strings.TrimSpace(d.URL), "/")
	d.StreamKey = strings.TrimSpace(d.StreamKey)
	if d.Name == "" || utf8.RuneCountInString(d.Name) > 60 {
		return Errorf(CodeBadRequest, "destination name must have 1..60 characters")
	}
	u, err := url.Parse(d.URL)
	if err != nil || (u.Scheme != "rtmp" && u.Scheme != "rtmps") || u.Host == "" {
		return Errorf(CodeBadRequest, "destination url must be rtmp:// or rtmps:// with a host")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Errorf(CodeBadRequest, "destination url must not carry credentials, query or fragment; use streamKey")
	}
	if strings.ContainsAny(d.StreamKey, "|[]' \t\n") || strings.ContainsAny(d.URL, "|[]' \t\n") {
		return Errorf(CodeBadRequest, "destination url and stream key must not contain | [ ] ' or spaces")
	}
	switch d.Kind {
	case "":
		d.Kind = inferKind(u.Host)
	case KindYouTube, KindTwitch, KindFacebook, KindCustom:
	default:
		return Errorf(CodeBadRequest, "destination kind %q is not youtube|twitch|facebook|custom", d.Kind)
	}
	return nil
}

// PublishURL is the full ingest URL (url + "/" + key) handed to the encoder.
func (d Destination) PublishURL() string {
	if d.StreamKey == "" {
		return d.URL
	}
	return d.URL + "/" + d.StreamKey
}

// MaskedKey shows only the last four characters of the stream key.
func (d Destination) MaskedKey() string {
	n := utf8.RuneCountInString(d.StreamKey)
	if n == 0 {
		return ""
	}
	if n <= 4 {
		return strings.Repeat("•", n)
	}
	runes := []rune(d.StreamKey)
	return strings.Repeat("•", 8) + string(runes[n-4:])
}

func inferKind(host string) DestinationKind {
	host = strings.ToLower(host)
	switch {
	case strings.Contains(host, "youtube.com"):
		return KindYouTube
	case strings.Contains(host, "twitch.tv") || strings.Contains(host, "live-video.net"):
		return KindTwitch
	case strings.Contains(host, "facebook.com") || strings.Contains(host, "fbcdn.net"):
		return KindFacebook
	}
	return KindCustom
}
