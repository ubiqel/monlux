package brightness

import (
	"errors"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/monitor"
)

type fakeDevice struct {
	current uint16
	maximum uint16
	err     error
	writes  []uint16
}

func (d *fakeDevice) GetBrightness() (uint16, uint16, error) {
	return d.current, d.maximum, d.err
}

func (d *fakeDevice) SetBrightness(value uint16) error {
	if d.err != nil {
		return d.err
	}

	d.current = value
	d.writes = append(d.writes, value)

	return nil
}

var errNoDevice = errors.New("no device")

func TestCurrentUsesFreshCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(SaveCache(Cache{
		4: {Identity: "A", Current: 40, Maximum: 100, UpdatedAt: time.Now()},
	}))

	raw, maximum, err := Current(&fakeDevice{err: errNoDevice}, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, true)
	is.NoErr(err)
	is.Equal(raw, uint16(40))
	is.Equal(maximum, uint16(100))
}

func TestCurrentReadsWhenNotUsable(t *testing.T) {
	isolateCache(t)

	now := time.Now()
	fresh := Entry{Identity: "A", Current: 40, Maximum: 100, UpdatedAt: now}
	stale := fresh
	stale.UpdatedAt = now.Add(-2 * TTL)

	tests := []struct {
		name     string
		entry    Entry
		monitor  monitor.Monitor
		useCache bool
	}{
		{"stale entry", stale, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, true},
		{"entry for another monitor", fresh, monitor.Monitor{I2CBus: 4, EDIDIdentity: "B"}, true},
		{"cache disabled", fresh, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.NoErr(SaveCache(Cache{4: tt.entry}))

			_, _, err := Current(&fakeDevice{err: errNoDevice}, tt.monitor, tt.useCache)
			is.True(err != nil)
		})
	}
}

func TestWriteFailureForgetsEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100)

	err := Write(&fakeDevice{err: errNoDevice}, m, 50, 100, true)
	is.True(err != nil)

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok) // a failed write leaves the monitor's state unknown
}

func TestWriteWithoutCacheLeavesCacheAlone(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100)

	is.True(Write(&fakeDevice{err: errNoDevice}, m, 50, 100, false) != nil)

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(40))
}

func TestStepMovesOnEveryPressOnSmallMaximum(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	// A 1% step on maximum 255 is 2.55 units, which percent arithmetic would
	// round back to the same raw value at some levels.
	dev := &fakeDevice{current: 122, maximum: 255}
	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	for range 5 {
		is.NoErr(Step(dev, m, 1, false))
	}

	is.Equal(len(dev.writes), 5)
	for i := 1; i < len(dev.writes); i++ {
		is.True(dev.writes[i] > dev.writes[i-1])
	}
}

func TestStepClampsAtEnds(t *testing.T) {
	isolateCache(t)
	is := is.New(t)
	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	up := &fakeDevice{current: 95, maximum: 100}
	is.NoErr(Step(up, m, 10, false))
	is.Equal(up.current, uint16(100))

	down := &fakeDevice{current: 5, maximum: 100}
	is.NoErr(Step(down, m, -10, false))
	is.Equal(down.current, uint16(0))
}

func TestStepByOnePercentOnHundredMaximum(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{current: 40, maximum: 100}
	is.NoErr(Step(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, -1, false))
	is.Equal(dev.current, uint16(39))
}

func TestPercentRoundTripsThroughRaw(t *testing.T) {
	is := is.New(t)

	for _, maximum := range []uint16{100, 255, 1000, 65535} {
		for pct := range 101 {
			is.Equal(Percent(Raw(pct, maximum), maximum), pct)
		}
	}
}

func TestClamp(t *testing.T) {
	is := is.New(t)

	is.Equal(Clamp(-5, 0, 100), 0)
	is.Equal(Clamp(120, 0, 100), 100)
	is.Equal(Clamp(40, 0, 100), 40)
}
