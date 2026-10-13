package brightness

import (
	"cmp"
	"fmt"
	"log/slog"
	"time"

	"github.com/ubiqel/monlux/internal/ddc"
	"github.com/ubiqel/monlux/internal/monitor"
)

// Device is what brightness needs from a monitor, so tests can use a fake.
type Device interface {
	GetBrightness() (current, maximum uint16, err error)
	SetBrightness(value uint16) error
}

func Current(dev Device, m monitor.Monitor, useCache bool) (current, maximum uint16, err error) {
	if !useCache {
		slog.Debug("Brightness cache disabled, reading monitor.", "bus", m.I2CBus)
	} else if e, ok := Lookup(m.I2CBus, m.EDIDIdentity); ok {
		age := time.Since(e.UpdatedAt)
		if e.Fresh(time.Now()) {
			slog.Debug("Brightness cache hit.", "bus", m.I2CBus, "raw", e.Current, "maximum", e.Maximum, "age", age)

			return e.Current, e.Maximum, nil
		}

		slog.Debug("Brightness cache entry is stale, reading monitor.", "bus", m.I2CBus, "age", age, "ttl", TTL)
	}

	current, maximum, err = dev.GetBrightness()
	if err != nil {
		return 0, 0, fmt.Errorf("reading brightness via i2c: %w", err)
	}

	if useCache {
		Store(m.I2CBus, m.EDIDIdentity, current, maximum)
	}

	return current, maximum, nil
}

// Write sets the raw brightness of m. A failed write forgets the cached value,
// since the monitor's state is unknown after it.
func Write(dev Device, m monitor.Monitor, raw, maximum uint16, useCache bool) error {
	if !useCache {
		slog.Debug("Brightness cache disabled, writing monitor.", "bus", m.I2CBus)
	}

	if err := dev.SetBrightness(raw); err != nil {
		if useCache {
			Forget(m.I2CBus)
		}

		return fmt.Errorf("writing brightness via i2c: %w", err)
	}

	if useCache {
		Store(m.I2CBus, m.EDIDIdentity, raw, maximum)
	}

	return nil
}

func SetPercent(dev Device, m monitor.Monitor, pct int, useCache bool) error {
	return adjust(dev, m, useCache, func(_, maximum uint16) uint16 {
		return Raw(pct, maximum)
	})
}

// Step moves in raw units, so a press always changes the value.
func Step(dev Device, m monitor.Monitor, delta int, useCache bool) error {
	return adjust(dev, m, useCache, func(raw, maximum uint16) uint16 {
		return stepRaw(raw, maximum, delta)
	})
}

func adjust(dev Device, m monitor.Monitor, useCache bool, next func(raw, maximum uint16) uint16) error {
	raw, maximum, err := Current(dev, m, useCache)
	if err != nil {
		return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
	}

	if err := Write(dev, m, next(raw, maximum), maximum, useCache); err != nil {
		return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", m.I2CBus, err)
	}

	return nil
}

func stepRaw(raw, maximum uint16, delta int) uint16 {
	if delta == 0 {
		return raw
	}

	size := delta
	if size < 0 {
		size = -size
	}

	// Without the minimum of one unit, a 1% step on a small maximum could round
	// to nothing and leave the value unchanged.
	span := max(1, (size*int(maximum)+50)/100)
	if delta < 0 {
		span = -span
	}

	return uint16(Clamp(int(raw)+span, 0, int(maximum)))
}

// Percent rounds to nearest. Truncating would read a value written as N% back
// as N-1% when the maximum isn't 100.
func Percent(raw, maximum uint16) int {
	return Clamp((int(raw)*100+int(maximum)/2)/int(maximum), 0, 100)
}

func Raw(pct int, maximum uint16) uint16 {
	pct = Clamp(pct, 0, 100)

	return uint16((pct*int(maximum) + 50) / 100)
}

type Reading struct {
	Percent int
	Asleep  bool
}

func Read(host monitor.Host, m monitor.Monitor, useCache bool) (Reading, error) {
	bus, err := ddc.Open(m.I2CBus)
	if err != nil {
		slog.Warn("Failed to open fd for I2C bus.", "bus", m.I2CBus, "error", err)

		return Reading{}, fmt.Errorf("opening fd for bus '%v': %w", m.I2CBus, err)
	}
	defer bus.Close()

	raw, maximum, err := Current(bus, m, useCache)
	if err != nil {
		_, asleep := host.DPMSState(m)

		return Reading{Asleep: asleep}, err
	}

	return Reading{Percent: Percent(raw, maximum)}, nil
}

func Clamp[T cmp.Ordered](value, minimum, maximum T) T {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}

	return value
}
