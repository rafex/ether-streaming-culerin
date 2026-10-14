// Package clock is the system implementation of ports.Clock.
package clock

import (
	"time"

	"github.com/rafex/ether-streaming-culerin/source/backend/go/bridge/internal/ports"
)

// System uses the real wall clock.
type System struct{}

// Now implements ports.Clock.
func (System) Now() time.Time { return time.Now() }

// AfterFunc implements ports.Clock.
func (System) AfterFunc(d time.Duration, f func()) ports.Timer { return time.AfterFunc(d, f) }
