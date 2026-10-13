package brightness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/ddc"
	"github.com/ubiqel/monlux/internal/monitor"
)

type fakeDevice struct {
	current uint16
	maximum uint16
	getErr  error
	setErr  error
	writes  []uint16
	closed  bool
}

func (d *fakeDevice) GetBrightness() (uint16, uint16, error) {
	return d.current, d.maximum, d.getErr
}

func (d *fakeDevice) SetBrightness(value uint16) error {
	if d.setErr != nil {
		return d.setErr
	}

	d.current = value
	d.writes = append(d.writes, value)

	return nil
}

func (d *fakeDevice) Close() error {
	d.closed = true

	return nil
}

var errNoDevice = errors.New("no device")

func TestCurrentUsesFreshCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	is.NoErr(SaveCache(Cache{
		4: {Identity: "A", Current: 40, Maximum: 100, UpdatedAt: time.Now()},
	}))

	raw, maximum, err := Current(&fakeDevice{getErr: errNoDevice}, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, true)
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

			_, _, err := Current(&fakeDevice{getErr: errNoDevice}, tt.monitor, tt.useCache)
			is.True(err != nil)
		})
	}
}

func TestCurrentPopulatesCacheOnMiss(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	raw, maximum, err := Current(&fakeDevice{current: 40, maximum: 100}, m, true)
	is.NoErr(err)
	is.Equal(raw, uint16(40))
	is.Equal(maximum, uint16(100))

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(40))
	is.Equal(e.Maximum, uint16(100))
}

func TestCurrentWithoutCacheDoesNotStore(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	raw, _, err := Current(&fakeDevice{current: 40, maximum: 100}, m, false)
	is.NoErr(err)
	is.Equal(raw, uint16(40))

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok)
}

func TestCurrentFutureEntryReadsDevice(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	is.NoErr(SaveCache(Cache{
		4: {Identity: "A", Current: 40, Maximum: 100, UpdatedAt: time.Now().Add(time.Minute)},
	}))

	raw, _, err := Current(&fakeDevice{current: 70, maximum: 100}, m, true)
	is.NoErr(err)
	is.Equal(raw, uint16(70))
}

func TestCurrentReadErrorIsWrapped(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	raw, maximum, err := Current(&fakeDevice{getErr: errNoDevice}, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, true)
	is.True(errors.Is(err, errNoDevice))
	is.Equal(raw, uint16(0))
	is.Equal(maximum, uint16(0))
}

func TestCurrentRejectsZeroMaximum(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	_, _, err := Current(&fakeDevice{current: 0, maximum: 0}, m, true)
	is.True(err != nil)

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok)
}

func TestCurrentClampsToMaximum(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	raw, maximum, err := Current(&fakeDevice{current: 120, maximum: 100}, m, true)
	is.NoErr(err)
	is.Equal(raw, uint16(100))
	is.Equal(maximum, uint16(100))

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(100))
}

func TestWriteSuccessStoresValue(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	dev := &fakeDevice{}

	is.NoErr(Write(dev, m, 50, 255, true))
	is.Equal(dev.writes, []uint16{50})

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(50))
	is.Equal(e.Maximum, uint16(255))
}

func TestWriteSuccessWithoutCacheDoesNotStore(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	is.NoErr(Write(&fakeDevice{}, m, 50, 100, false))

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok)
}

func TestWriteFailureForgetsEntry(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100)

	err := Write(&fakeDevice{setErr: errNoDevice}, m, 50, 100, true)
	is.True(err != nil)

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok) // a failed write leaves the monitor's state unknown
}

func TestWriteWithoutCacheLeavesCacheAlone(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100)

	is.True(Write(&fakeDevice{setErr: errNoDevice}, m, 50, 100, false) != nil)

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(40))
}

func TestWriteErrorKeepsCause(t *testing.T) {
	is := is.New(t)

	err := Write(&fakeDevice{setErr: ddc.ErrUnsupportedFeature}, monitor.Monitor{I2CBus: 4}, 50, 100, false)
	is.True(errors.Is(err, ddc.ErrUnsupportedFeature))
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

func TestStepDownMovesOnEveryPressOnSmallMaximum(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{current: 10, maximum: 255}
	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	for range 3 {
		is.NoErr(Step(dev, m, -1, false))
	}

	is.Equal(dev.writes, []uint16{7, 4, 1})
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

func TestStepLargeDeltaClampsAtEnds(t *testing.T) {
	isolateCache(t)
	is := is.New(t)
	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	up := &fakeDevice{current: 40, maximum: 100}
	is.NoErr(Step(up, m, 500, false))
	is.Equal(up.current, uint16(100))

	down := &fakeDevice{current: 40, maximum: 100}
	is.NoErr(Step(down, m, -500, false))
	is.Equal(down.current, uint16(0))
}

func TestStepZeroDeltaRewritesSameValue(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{current: 40, maximum: 100}
	is.NoErr(Step(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, 0, false))
	is.Equal(dev.writes, []uint16{40})
}

func TestStepByOnePercentOnHundredMaximum(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{current: 40, maximum: 100}
	is.NoErr(Step(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, -1, false))
	is.Equal(dev.current, uint16(39))
}

func TestStepReadFailureWritesNothing(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{current: 40, maximum: 100, getErr: errNoDevice}

	err := Step(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, 1, false)
	is.True(errors.Is(err, errNoDevice))
	is.Equal(len(dev.writes), 0)
}

func TestStepWriteFailureForgetsCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100)

	err := Step(&fakeDevice{current: 40, maximum: 100, setErr: errNoDevice}, m, 1, true)
	is.True(errors.Is(err, errNoDevice))

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok)
}

func TestSetPercentWritesRawValue(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{maximum: 255}

	is.NoErr(SetPercent(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, 50, false))
	is.Equal(dev.writes, []uint16{128})
}

func TestSetPercentClampsRange(t *testing.T) {
	isolateCache(t)

	tests := []struct {
		name string
		pct  int
		want uint16
	}{
		{"below zero", -5, 0},
		{"above one hundred", 150, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			dev := &fakeDevice{maximum: 100}
			is.NoErr(SetPercent(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, tt.pct, false))
			is.Equal(dev.writes, []uint16{tt.want})
		})
	}
}

func TestSetPercentUpdatesCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}

	is.NoErr(SetPercent(&fakeDevice{maximum: 100}, m, 60, true))

	e, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(ok)
	is.Equal(e.Current, uint16(60))
}

func TestSetPercentReadFailureWritesNothing(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{maximum: 100, getErr: errNoDevice}

	err := SetPercent(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, 60, false)
	is.True(errors.Is(err, errNoDevice))
	is.Equal(len(dev.writes), 0)
}

func TestSetPercentWriteFailureForgetsCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 40, 100)

	err := SetPercent(&fakeDevice{current: 40, maximum: 100, setErr: errNoDevice}, m, 60, true)
	is.True(errors.Is(err, errNoDevice))

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok)
}

func TestRawGoldenValues(t *testing.T) {
	tests := []struct {
		name    string
		pct     int
		maximum uint16
		want    uint16
	}{
		{"zero", 0, 255, 0},
		{"half of 255 rounds to nearest", 50, 255, 128},
		{"full", 100, 255, 255},
		{"full on 16-bit maximum", 100, 65535, 65535},
		{"below zero clamps", -5, 100, 0},
		{"above one hundred clamps", 150, 100, 100},
		{"zero maximum", 50, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(Raw(tt.pct, tt.maximum), tt.want)
		})
	}
}

func TestPercentGoldenValues(t *testing.T) {
	tests := []struct {
		name    string
		raw     uint16
		maximum uint16
		want    int
	}{
		{"zero", 0, 255, 0},
		{"rounds down", 1, 255, 0},
		{"rounds up", 2, 255, 1},
		{"127 of 255 is 49.8% and reads as 50", 127, 255, 50},
		{"128 of 255 is 50.2% and reads as 50", 128, 255, 50},
		{"full", 255, 255, 100},
		{"above maximum clamps", 120, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(Percent(tt.raw, tt.maximum), tt.want)
		})
	}
}

func TestPercentRoundTripsThroughRaw(t *testing.T) {
	is := is.New(t)

	for _, maximum := range []uint16{100, 255, 1000, 65535} {
		for pct := range 101 {
			is.Equal(Percent(Raw(pct, maximum), maximum), pct)
		}
	}
}

func TestRawIsMonotonic(t *testing.T) {
	is := is.New(t)

	for _, maximum := range []uint16{100, 255, 1000} {
		prev := uint16(0)
		for pct := range 101 {
			r := Raw(pct, maximum)
			is.True(r >= prev)
			prev = r
		}
	}
}

func TestClamp(t *testing.T) {
	is := is.New(t)

	is.Equal(Clamp(-5, 0, 100), 0)
	is.Equal(Clamp(120, 0, 100), 100)
	is.Equal(Clamp(40, 0, 100), 40)
}

func readHost(t *testing.T, dev *fakeDevice, dpms string) (monitor.Host, monitor.Monitor) {
	t.Helper()

	drm := t.TempDir()
	connector := "card1-DP-2"

	if dpms != "" {
		dir := filepath.Join(drm, connector)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "dpms"), []byte(dpms+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	host := monitor.Host{
		DRMPath: drm,
		OpenBus: func(int) (monitor.Bus, error) { return dev, nil },
	}

	return host, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A", DRMConnector: &connector}
}

func TestReadReturnsPercent(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{current: 128, maximum: 255}
	host, m := readHost(t, dev, "On")

	r, err := Read(host, m, false)
	is.NoErr(err)
	is.Equal(r, Reading{Percent: 50})
	is.True(dev.closed)
}

func TestReadOpenFailureSkipsDPMSCheck(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	// The display is asleep, but the bus never opened, so Read must not claim it.
	host, m := readHost(t, &fakeDevice{}, "Off")
	host.OpenBus = func(int) (monitor.Bus, error) { return nil, errNoDevice }

	r, err := Read(host, m, false)
	is.True(errors.Is(err, errNoDevice))
	is.Equal(r, Reading{})
}

func TestReadFailureReportsAsleep(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	dev := &fakeDevice{getErr: errNoDevice}
	host, m := readHost(t, dev, "Off")

	r, err := Read(host, m, false)
	is.True(errors.Is(err, errNoDevice))
	is.Equal(r, Reading{Asleep: true})
	is.True(dev.closed)
}

func TestReadFailureWhileAwakeIsNotAsleep(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	tests := []struct {
		name string
		dpms string
	}{
		{"on", "On"},
		{"state file missing", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			host, m := readHost(t, &fakeDevice{getErr: errNoDevice}, tt.dpms)

			r, err := Read(host, m, false)
			is.True(err != nil)
			is.Equal(r.Asleep, false)
		})
	}
}

func TestReadZeroMaximumIsError(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	host, m := readHost(t, &fakeDevice{current: 0, maximum: 0}, "On")

	r, err := Read(host, m, false)
	is.True(err != nil)
	is.Equal(r, Reading{})
}
