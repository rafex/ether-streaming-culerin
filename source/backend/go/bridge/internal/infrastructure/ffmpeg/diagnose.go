package ffmpeg

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

// Why a publisher died, as far as FFmpeg's own words tell. It is what an operator needs first
// to decide whether the fault is ours (encoder, stall) or the platform's (it closed the stream,
// rejected the key, the network dropped).
const (
	kindRemoteClosed = "remote-closed" // the platform closed the connection (EPIPE / reset by peer)
	kindTimeout      = "timeout"       // nothing went through in time
	kindRejected     = "rejected"      // handshake or key refused (4xx, auth)
	kindDNS          = "dns"           // the ingest host did not resolve
	kindRefused      = "refused"       // connection refused / unreachable
	kindEncoder      = "encoder"       // FFmpeg itself failed (invalid data, no space)
	kindUnknown      = "unknown"
)

type exitDiagnosis struct {
	kind string
	// slave is the tee slave FFmpeg blamed ("Slave muxer #N failed"), or -1.
	slave int
}

var (
	slavePattern = regexp.MustCompile(`[Ss]lave muxer #(\d+)`)
	patterns     = []struct {
		kind string
		re   *regexp.Regexp
	}{
		{kindDNS, regexp.MustCompile(`(?i)name or service not known|failed to resolve|could not resolve|no address associated|temporary failure in name resolution`)},
		{kindRefused, regexp.MustCompile(`(?i)connection refused|no route to host|network is unreachable`)},
		{kindRejected, regexp.MustCompile(`(?i)server returned [45]\d\d|handshake failed|authentication|unauthorized|forbidden|rtmp_connect|publish.*(denied|failed)`)},
		{kindTimeout, regexp.MustCompile(`(?i)timed out|timeout`)},
		{kindRemoteClosed, regexp.MustCompile(`(?i)broken pipe|connection reset|return code -32|-32\)|end of file|stream ended|error writing (trailer|header)`)},
		{kindEncoder, regexp.MustCompile(`(?i)invalid data found|no space left|conversion failed|error while (decoding|filtering)|cannot allocate memory`)},
	}
)

// classifyExit reads FFmpeg's last lines (newest evidence wins) and names the cause.
func classifyExit(lines []string) exitDiagnosis {
	d := exitDiagnosis{kind: kindUnknown, slave: -1}
	for i := len(lines) - 1; i >= 0; i-- {
		if m := slavePattern.FindStringSubmatch(lines[i]); m != nil && d.slave < 0 {
			d.slave, _ = strconv.Atoi(m[1])
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		for _, p := range patterns {
			if p.re.MatchString(lines[i]) {
				d.kind = p.kind
				return d
			}
		}
	}
	return d
}

const (
	restartBase = 2 * time.Second
	restartMax  = 30 * time.Second
	// stableAfter: a publisher that lived this long was healthy, so the next failure starts the
	// backoff over instead of continuing it.
	stableAfter = 60 * time.Second
)

// restartDelay is how long to wait before restarting a publisher that has failed `streak` times
// in a row: 2 s, 4 s, 8 s ... capped at 30 s. A flapping destination is retried less and less
// often instead of being hammered every 2 s (platforms may block a key that reconnects too fast).
func restartDelay(streak int) time.Duration {
	if streak <= 1 {
		return restartBase
	}
	d := restartBase
	for i := 1; i < streak && d < restartMax; i++ {
		d *= 2
	}
	return min(d, restartMax)
}

// nextStreak decides the failure streak after a publisher that lived `lived` died.
func nextStreak(prev int, lived time.Duration) int {
	if lived >= stableAfter {
		return 1
	}
	return prev + 1
}

type watchdogAction int

const (
	watchOK         watchdogAction = iota
	watchRestartRun                // start a new compositor run
	watchRestartAll                // restart the publisher too
)

// Variables, not constants, so the tests can run the watch in seconds.
var (
	// stallAfter: on air but no media tag written for this long = the output is frozen.
	stallAfter = 10 * time.Second
	// stallCooldown spaces the repairs so a slow start is not mistaken for another stall.
	stallCooldown = 15 * time.Second
)

// stallEscalateAfter: this many run restarts without recovery restart the publisher too.
const stallEscalateAfter = 2

// watchdog decides what to do about a frozen output. tagAge is the time since the last media tag
// was written (zero when nothing has gone on air yet), sinceRepair the time since the last
// repair attempt, attempts how many it has made in this episode.
func watchdog(onAir bool, tagAge, sinceRepair time.Duration, attempts int) watchdogAction {
	if !onAir || tagAge < stallAfter || sinceRepair < stallCooldown {
		return watchOK
	}
	if attempts >= stallEscalateAfter {
		return watchRestartAll
	}
	return watchRestartRun
}

// planDiff says in a few words what changed between two plans: which video sources entered or
// left the picture and which tiles went to a card (a source that stopped delivering).
func planDiff(old, next domain.CompositionPlan) string {
	var parts []string
	oldV, newV := old.VideoSources(), next.VideoSources()
	var added, removed []string
	for _, ref := range newV {
		if !slices.Contains(oldV, ref) {
			added = append(added, ref.String())
		}
	}
	for _, ref := range oldV {
		if !slices.Contains(newV, ref) {
			removed = append(removed, ref.String())
		}
	}
	if len(added) > 0 {
		parts = append(parts, "video+="+strings.Join(added, ","))
	}
	if len(removed) > 0 {
		parts = append(parts, "video-="+strings.Join(removed, ","))
	}
	if len(old.Tiles) != len(next.Tiles) {
		parts = append(parts, "tiles="+strconv.Itoa(len(old.Tiles))+"->"+strconv.Itoa(len(next.Tiles)))
	}
	if len(old.Audio) != len(next.Audio) {
		parts = append(parts, "audio="+strconv.Itoa(len(old.Audio))+"->"+strconv.Itoa(len(next.Audio)))
	}
	if len(parts) == 0 {
		return "layout/title/output"
	}
	return strings.Join(parts, " ")
}
