// Package brightness reads and sets monitor brightness, optionally through a
// cache that avoids talking to the monitor on every command.
package brightness

import (
	"cmp"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/monlux/internal/i2c"
	"github.com/ubiqel/monlux/internal/monitor"
	"golang.org/x/sys/unix"
)

// Current returns the brightness of m as a percentage and its raw maximum. With
// useCache, a fresh cached value is used instead of reading the monitor.
func Current(fd int, m monitor.Monitor, useCache bool) (pct int, maximum uint16, err error) {
	if !useCache {
		slog.Debug("Brightness cache disabled, reading monitor.", "bus", m.I2CBus)
	} else if e, ok := Lookup(m.I2CBus, m.EDIDIdentity); ok {
		age := time.Since(e.UpdatedAt)
		if e.Fresh(time.Now()) {
			slog.Debug("Brightness cache hit.", "bus", m.I2CBus, "percent", e.Percent(), "age", age)

			return e.Percent(), e.Maximum, nil
		}

		slog.Debug("Brightness cache entry is stale, reading monitor.", "bus", m.I2CBus, "age", age, "ttl", TTL)
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
	if !useCache {
		slog.Debug("Brightness cache disabled, writing monitor.", "bus", m.I2CBus)
	}

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

// SetPercent sets the brightness of m to pct, clamped to 0-100.
func SetPercent(fd int, m monitor.Monitor, pct int, useCache bool) error {
	return adjust(fd, m, useCache, func(int) int { return pct })
}

// Step changes the brightness of m by delta percentage points, clamped to
// 0-100. A negative delta lowers it.
func Step(fd int, m monitor.Monitor, delta int, useCache bool) error {
	return adjust(fd, m, useCache, func(curr int) int { return curr + delta })
}

// adjust reads the brightness of m, computes the new percentage from it with
// next, and writes that percentage back.
func adjust(fd int, m monitor.Monitor, useCache bool, next func(curr int) int) error {
	currPct, maximum, err := Current(fd, m, useCache)
	if err != nil {
		return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
	}

	pct := Clamp(next(currPct), 0, 100)
	raw := uint16(int(maximum) * pct / 100)
	if err := Write(fd, m, raw, maximum, useCache); err != nil {
		return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", m.I2CBus, err)
	}

	return nil
}

// Reading is the brightness of a monitor as shown by "monlux list".
type Reading struct {
	Percent int
	// Asleep is set when the read failed because the display is in DPMS sleep.
	Asleep bool
}

// Read returns the brightness of m, honouring the cache like Current. If the
// read fails while the display is asleep, the error is returned together with
// Reading.Asleep set.
func Read(host monitor.Host, m monitor.Monitor, useCache bool) (Reading, error) {
	fd, err := i2c.Open(m.I2CBus)
	if err != nil {
		slog.Warn("Failed to open fd for I2C bus.", "bus", m.I2CBus, "error", err)

		return Reading{}, fmt.Errorf("opening fd for bus '%v': %w", m.I2CBus, err)
	}
	defer unix.Close(fd)

	pct, _, err := Current(fd, m, useCache)
	if err != nil {
		_, asleep := host.DPMSState(m)

		return Reading{Asleep: asleep}, err
	}

	return Reading{Percent: pct}, nil
}

// Observe returns the percentage to report for a value just read from m, and
// records the value. A read that disagrees with a write made moments ago is
// ignored, since the monitor probably hasn't applied the write yet.
func Observe(m monitor.Monitor, pct int, raw, maximum uint16) int {
	if e, ok := Lookup(m.I2CBus, m.EDIDIdentity); ok && e.DisagreesWithRecentWrite(raw, time.Now()) {
		slog.Debug("Ignoring brightness read that disagrees with a recent write.", "bus", m.I2CBus, "raw", raw, "written_raw", e.Current)

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
