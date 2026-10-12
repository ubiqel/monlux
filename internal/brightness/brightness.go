// Package brightness reads and sets monitor brightness, optionally through a
// cache that avoids talking to the monitor on every command.
package brightness

import (
	"cmp"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/monitor"
)

// Current returns the brightness of m as a percentage and its raw maximum. With
// useCache, a fresh cached value is used instead of reading the monitor.
func Current(fd int, m monitor.Monitor, useCache bool) (pct int, maximum uint16, err error) {
	if useCache {
		slog.Debug("Trying to find brightness value in cache", "edid", m.EDIDIdentity)
		if e, ok := Lookup(m.I2CBus, m.EDIDIdentity); ok && e.Fresh(time.Now()) {
			return e.Percent(), e.Maximum, nil
		}
	}

	pct, raw, maximum, err := i2c.ReadBrightness(fd)
	if err != nil {
		return 0, 0, fmt.Errorf("reading brightness via i2c: %w", err)
	}

	if useCache {
		Store(m.I2CBus, m.EDIDIdentity, raw, maximum, false)
	}

	return pct, maximum, nil
}

// Write sets the raw brightness of m. A failed write forgets the cached value,
// since the monitor's state is unknown after it.
func Write(fd int, m monitor.Monitor, raw, maximum uint16, useCache bool) error {
	if err := i2c.SetBrightness(fd, raw); err != nil {
		if useCache {
			Forget(m.I2CBus)
		}

		return fmt.Errorf("writing brightness via i2c: %w", err)
	}

	if useCache {
		Store(m.I2CBus, m.EDIDIdentity, raw, maximum, true)
	}

	return nil
}

// Observe returns the percentage to report for a value just read from m, and
// records the value. A read that disagrees with a write made moments ago is
// ignored, since the monitor probably hasn't applied the write yet.
func Observe(m monitor.Monitor, pct int, raw, maximum uint16) int {
	if e, ok := Lookup(m.I2CBus, m.EDIDIdentity); ok && e.DisagreesWithRecentWrite(raw, time.Now()) {
		return e.Percent()
	}

	Store(m.I2CBus, m.EDIDIdentity, raw, maximum, false)

	return pct
}

// Clamp limits value to the range [minimum, maximum].
func Clamp[T cmp.Ordered](value, minimum, maximum T) T {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}

	return value
}
