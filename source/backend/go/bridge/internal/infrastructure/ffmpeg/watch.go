package ffmpeg

import (
	"fmt"
	"strings"
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/domain"
)

// healthLoop watches one publisher: every healthEvery it reads the meter, reports (a summary every
// healthSummary when healthy, a warning on every check while something looks wrong) and repairs a
// frozen output. It ends with the publisher.
func (p *pipeline) healthLoop(proc *process, meter *healthMeter) {
	ticker := time.NewTicker(healthEvery)
	defer ticker.Stop()
	var ticks, unhealthy int
	for {
		select {
		case <-proc.done:
			return
		case <-ticker.C:
		}

		p.mu.Lock()
		if p.stopped || p.publish != proc {
			p.mu.Unlock()
			return
		}
		onAir := !p.onAirAt.IsZero()
		run := p.onAir
		onAirFor := time.Duration(0)
		if onAir {
			onAirFor = time.Since(p.onAirAt)
		}
		refs := append(p.plan.VideoSources(), p.plan.Audio...)
		p.mu.Unlock()

		sample := meter.sample()
		inputs := p.inputAges(refs)
		problems := sample.problems(onAir)
		ticks++

		p.mu.Lock()
		if p.stopped || p.publish != proc {
			p.mu.Unlock()
			return
		}
		p.outKbps = sample.kbps()
		action := watchOK
		if len(problems) == 0 {
			unhealthy = 0
			p.repairs = 0
		} else {
			unhealthy++
			tagAge := max(sample.tagAge, 0)
			action = watchdog(onAir, tagAge, time.Since(p.repairAt), p.repairs)
		}
		fields := []any{"room", p.room, "run", run, "on_air_for", onAirFor.Round(time.Second).String(),
			"out_kbps", int(sample.kbps() + 0.5), "video_tags_per_s", fmt.Sprintf("%.1f", sample.videoFPS()),
			"last_tag_age_ms", sample.tagAge.Milliseconds(), "max_write_ms", sample.maxWrite.Milliseconds(),
			"sent_bytes", sample.totalBytes, "inputs", inputs}
		switch {
		case len(problems) > 0:
			// Every check for the first few, then every sixth: a long outage must not flood the log.
			if unhealthy <= 3 || unhealthy%6 == 0 {
				p.c.log.Warn("broadcast unhealthy", append(fields, "problems", strings.Join(problems, "; "),
					"checks_unhealthy", unhealthy, "restarts", p.restarts)...)
			}
		case ticks%int(healthSummary/healthEvery) == 0:
			p.c.log.Info("broadcast health", append(fields, "restarts", p.restarts)...)
		}
		p.repairLocked(action, sample)
		p.mu.Unlock()
	}
}

// repairLocked acts on the watchdog's decision: first a fresh compositor run (the frozen one is
// replaced as soon as the new one produces a frame); if that does not bring the output back, the
// publisher is restarted as well.
func (p *pipeline) repairLocked(action watchdogAction, s healthSample) {
	switch action {
	case watchRestartRun:
		p.repairs++
		p.repairAt = time.Now()
		p.lastErrKind, p.lastErrAt = "stall", time.Now()
		p.lastErr = fmt.Sprintf("output stalled %.0fs: restarted the compositor", s.tagAge.Seconds())
		p.restarts++
		p.c.log.Error("output stalled; restarting the compositor run", "room", p.room,
			"tag_age_ms", s.tagAge.Milliseconds(), "attempt", p.repairs)
		if err := p.startComposeLocked(); err != nil {
			p.lastErr = err.Error()
			p.c.log.Error("could not restart the compositor", "room", p.room, "err", err)
		}
	case watchRestartAll:
		p.failStreak = nextStreak(p.failStreak, time.Since(p.pubStarted))
		delay := restartDelay(p.failStreak)
		p.repairs = 0
		p.repairAt = time.Now()
		p.lastErrKind, p.lastErrAt = "stall", time.Now()
		p.lastErr = fmt.Sprintf("output stalled %.0fs and the compositor restarts did not help: restarted the publisher", s.tagAge.Seconds())
		p.restarts++
		p.c.log.Error("output still stalled; restarting the publisher", "room", p.room,
			"tag_age_ms", s.tagAge.Milliseconds(), "fail_streak", p.failStreak, "retry_in", delay.String())
		p.killLocked()
		time.AfterFunc(delay, p.restart)
	}
}

// inputAges reports how long ago each input of the plan last delivered a packet, which is what
// explains a frozen tile or a silent mic: "p-1/camera=0.1s p-1/screen=12.4s". A source that never
// delivered shows "never".
func (p *pipeline) inputAges(refs []domain.SourceRef) string {
	seen := map[domain.SourceRef]bool{}
	var parts []string
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		last := p.c.tap.LastPacket(p.room, ref)
		if last.IsZero() {
			parts = append(parts, ref.String()+"=never")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%.1fs", ref.String(), time.Since(last).Seconds()))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}
