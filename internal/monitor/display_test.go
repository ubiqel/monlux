package monitor

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/ddc"
)

// fakeBus records whether WithDisplay closed it, so tests can check that the
// bus is released on every path.
type fakeBus struct {
	closed bool
}

func (b *fakeBus) GetBrightness() (uint16, uint16, error) {
	return 0, 100, nil
}

func (b *fakeBus) SetBrightness(uint16) error {
	return nil
}

func (b *fakeBus) Close() error {
	b.closed = true

	return nil
}

// displayFixture has card0-DP-2 on bus 4, card0-DP-3 on bus 5, and bus 6 with
// no connector.
func displayFixture(t *testing.T) Host {
	t.Helper()

	host, _ := installFixture(t, fixture{
		gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4, 5, 6}}},
		connectors: []fxConnector{
			{name: "card0-DP-2", gpu: 0, status: "connected", edid: makeEDID("ALPHA"), ddcBus: 4},
			{name: "card0-DP-3", gpu: 0, status: "connected", edid: makeEDID("BRAVO"), ddcBus: 5},
		},
		buses: map[int]fxBus{
			4: {edid: makeEDID("ALPHA"), ddc: true},
			5: {edid: makeEDID("BRAVO"), ddc: true},
			6: {edid: makeEDID("CHARLIE"), ddc: true},
		},
	})
	host.NoCache = true

	return host
}

func setDPMS(t *testing.T, host Host, connector, state string) {
	t.Helper()

	mustWrite(t, filepath.Join(host.DRMPath, connector, "dpms"), []byte(state+"\n"))
}

func recordingOpener(bus Bus, opened *[]int) func(int) (Bus, error) {
	return func(n int) (Bus, error) {
		*opened = append(*opened, n)

		return bus, nil
	}
}

func TestResolve(t *testing.T) {
	is := is.New(t)
	host := displayFixture(t)

	alphaID, ok := ddc.ParseIdentity(makeEDID("ALPHA"))
	is.True(ok)

	tests := []struct {
		name    string
		sel     Selector
		wantBus int
		wantErr string
	}{
		{name: "connector", sel: Selector{Type: IDTypeDRM, Value: "card0-DP-3"}, wantBus: 5},
		{name: "edid identity", sel: Selector{Type: IDTypeEDID, Value: alphaID}, wantBus: 4},
		{name: "i2c bus", sel: Selector{Type: IDTypeI2C, Value: "6"}, wantBus: 6},
		{name: "unknown connector", sel: Selector{Type: IDTypeDRM, Value: "card0-HDMI-A-1"}, wantErr: "no monitor matches"},
		// The monitor on bus 6 has no connector and must not match an empty one.
		{name: "empty connector", sel: Selector{Type: IDTypeDRM, Value: ""}, wantErr: "no monitor matches"},
		{name: "unknown bus", sel: Selector{Type: IDTypeI2C, Value: "9"}, wantErr: "no monitor matches"},
		{name: "unknown id type", sel: Selector{Type: IDType(99), Value: "card0-DP-2"}, wantErr: "unknown id type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			m, err := host.resolve(tt.sel)
			if tt.wantErr != "" {
				is.True(err != nil)
				is.True(strings.Contains(err.Error(), tt.wantErr))

				return
			}

			is.NoErr(err)
			is.Equal(m.I2CBus, tt.wantBus)
		})
	}
}

func TestResolveAmbiguousIdentity(t *testing.T) {
	is := is.New(t)

	// Two monitors share an EDID and have no connector, so the identity alone
	// can't pick one. i2c:<bus> must still pick each of them.
	host, _ := installFixture(t, fixture{
		buses: map[int]fxBus{
			4: {edid: makeEDID("ALPHA"), ddc: true},
			5: {edid: makeEDID("ALPHA"), ddc: true},
		},
	})
	host.NoCache = true

	alphaID, ok := ddc.ParseIdentity(makeEDID("ALPHA"))
	is.True(ok)

	_, err := host.resolve(Selector{Type: IDTypeEDID, Value: alphaID})
	is.True(err != nil)
	is.True(strings.Contains(err.Error(), "matches 2 monitors"))

	m, err := host.resolve(Selector{Type: IDTypeI2C, Value: "5"})
	is.NoErr(err)
	is.Equal(m.I2CBus, 5)
}

func TestDPMSState(t *testing.T) {
	tests := []struct {
		name       string
		dpms       []byte // nil leaves the file out
		wantState  string
		wantAsleep bool
	}{
		{name: "on", dpms: []byte("On\n"), wantState: "On"},
		{name: "off", dpms: []byte("Off\n"), wantState: "Off", wantAsleep: true},
		{name: "standby", dpms: []byte("Standby\n"), wantState: "Standby", wantAsleep: true},
		{name: "suspend", dpms: []byte("Suspend\n"), wantState: "Suspend", wantAsleep: true},
		{name: "empty file is not asleep", dpms: []byte{}},
		{name: "missing file is not asleep"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			drm := t.TempDir()
			connector := "card0-DP-2"
			mustMkdir(t, filepath.Join(drm, connector))

			if tt.dpms != nil {
				mustWrite(t, filepath.Join(drm, connector, "dpms"), tt.dpms)
			}

			host := Host{DRMPath: drm}
			state, asleep := host.DPMSState(Monitor{DRMConnector: &connector})
			is.Equal(state, tt.wantState)
			is.Equal(asleep, tt.wantAsleep)
		})
	}
}

func TestDPMSStateWithoutConnector(t *testing.T) {
	is := is.New(t)

	state, asleep := Host{DRMPath: t.TempDir()}.DPMSState(Monitor{})
	is.Equal(state, "")
	is.True(!asleep)
}

func TestWithDisplayPassesSelectedMonitor(t *testing.T) {
	is := is.New(t)
	host := displayFixture(t)

	bus := &fakeBus{}

	var opened []int

	host.OpenBus = recordingOpener(bus, &opened)

	var gotBus Bus

	var gotMonitor Monitor

	err := host.WithDisplay(Selector{Type: IDTypeI2C, Value: "5"}, func(b Bus, m Monitor) error {
		gotBus, gotMonitor = b, m

		return nil
	})
	is.NoErr(err)
	is.Equal(opened, []int{5})
	is.True(gotBus == bus)
	is.Equal(gotMonitor.I2CBus, 5)
	is.True(bus.closed)
}

func TestWithDisplayReturnsCallbackError(t *testing.T) {
	is := is.New(t)
	host := displayFixture(t)
	setDPMS(t, host, "card0-DP-2", "On")

	bus := &fakeBus{}

	var opened []int

	host.OpenBus = recordingOpener(bus, &opened)

	errBoom := errors.New("transfer failed")

	err := host.WithDisplay(Selector{Type: IDTypeDRM, Value: "card0-DP-2"}, func(Bus, Monitor) error {
		return errBoom
	})
	is.True(errors.Is(err, errBoom))
	is.True(!strings.Contains(err.Error(), "asleep"))
	is.True(bus.closed)
}

func TestWithDisplayReportsSleepingDisplay(t *testing.T) {
	is := is.New(t)
	host := displayFixture(t)
	setDPMS(t, host, "card0-DP-2", "Off")

	bus := &fakeBus{}

	var opened []int

	host.OpenBus = recordingOpener(bus, &opened)

	errBoom := errors.New("transfer failed")

	err := host.WithDisplay(Selector{Type: IDTypeDRM, Value: "card0-DP-2"}, func(Bus, Monitor) error {
		return errBoom
	})
	is.True(errors.Is(err, errBoom))
	is.True(strings.Contains(err.Error(), "display is asleep (DPMS Off)"))
	is.True(bus.closed)
}

func TestWithDisplayUnknownMonitorOpensNothing(t *testing.T) {
	is := is.New(t)
	host := displayFixture(t)

	var opened []int

	host.OpenBus = recordingOpener(&fakeBus{}, &opened)

	called := false
	err := host.WithDisplay(Selector{Type: IDTypeDRM, Value: "card0-HDMI-A-1"}, func(Bus, Monitor) error {
		called = true

		return nil
	})
	is.True(err != nil)
	is.Equal(len(opened), 0)
	is.True(!called)
}

func TestWithDisplayOpenError(t *testing.T) {
	is := is.New(t)
	host := displayFixture(t)

	errBoom := errors.New("no such device")
	host.OpenBus = func(int) (Bus, error) { return nil, errBoom }

	called := false
	err := host.WithDisplay(Selector{Type: IDTypeDRM, Value: "card0-DP-2"}, func(Bus, Monitor) error {
		called = true

		return nil
	})
	is.True(errors.Is(err, errBoom))
	is.True(!called)
}
