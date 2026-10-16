package domain

import (
	"net"
	"net/netip"
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

// internalSuffixes are names that only resolve inside a cluster or LAN.
var internalSuffixes = []string{".local", ".localhost", ".internal", ".lan", ".home", ".corp", ".svc", ".cluster.local"}

// CheckPublicHost rejects destination hosts that point inside the bridge's own
// network (loopback, private, link-local, CGNAT, unspecified, multicast
// addresses, "localhost" and cluster/LAN-only names). A destination makes the
// bridge open a connection to the host, so without this check the admin API
// would be a way to reach internal services. It cannot see through DNS: a
// public name resolving to a private address needs an egress network policy.
func CheckPublicHost(host string) error {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return Errorf(CodeBadRequest, "destination host is empty")
	}
	if host == "localhost" || !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return Errorf(CodeBadRequest, "destination host %q is not a public name", host)
	}
	for _, suffix := range internalSuffixes {
		if strings.HasSuffix(host, suffix) {
			return Errorf(CodeBadRequest, "destination host %q is an internal name", host)
		}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil // a name; DNS is the egress policy's job
	}
	addr = addr.Unmap()
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() || cgnat.Contains(addr) {
		return Errorf(CodeBadRequest, "destination host %s is not a public address", addr)
	}
	return nil
}

// Hostname is the host of the destination URL without the port.
func (d Destination) Hostname() string {
	u, err := url.Parse(d.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
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
