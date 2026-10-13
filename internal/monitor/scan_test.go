package monitor

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/matryer/is"
)

func edidWith(seed byte) [128]byte {
	var e [128]byte
	for i := range e {
		e[i] = seed + byte(i)
	}

	return e
}

func TestScanFixtures(t *testing.T) {
	edidA := makeEDID("ALPHA")
	edidB := makeEDID("BRAVO")
	edidOld := makeEDID("OLD")
	edidBad := edidWith(1) // no valid EDID header, so it can't be parsed

	tests := []struct {
		name  string
		fx    fixture
		check func(is *is.I, res ScanResult, reads []int)
	}{
		{
			name: "AMD with direct DDC links",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4, 5}}},
				connectors: []fxConnector{
					{name: "card0-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: 4},
					{name: "card0-DP-3", gpu: 0, status: "connected", edid: edidB, ddcBus: 5},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					5: {edid: edidB, ddc: true},
				},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4, 5})

				r := reportFor(is, res, 4)
				is.Equal(r.Connector, "card0-DP-2")
				is.Equal(r.Link, "ddc")
				is.Equal(r.Sysfs, "match")
				is.Equal(r.Result, "monitor")

				is.Equal(len(res.Unlinked), 0)
			},
		},
		{
			name: "NVIDIA ignores its wrong disconnected status",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:01:00.0", driver: "nvidia", card: "card1", buses: []int{4, 5}}},
				connectors: []fxConnector{
					{name: "card1-DP-2", gpu: 0, status: "disconnected", edid: edidA, ddcBus: -1},
					{name: "card1-DP-3", gpu: 0, status: "connected", edid: edidB, ddcBus: -1},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					5: {edid: edidB, ddc: true},
				},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4, 5})

				r := reportFor(is, res, 4)
				is.Equal(r.Connector, "card1-DP-2")
				is.Equal(r.Link, "edid")
			},
		},
		{
			name: "EDID that answers but no DDC/CI is rejected",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
				connectors: []fxConnector{
					{name: "card0-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: 4},
				},
				buses: map[int]fxBus{4: {edid: edidA, ddc: false}},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(len(res.monitors), 0)

				r := reportFor(is, res, 4)
				is.True(r.Probed)
				is.Equal(r.DDC, false)
				is.Equal(r.Result, "EDID only, DDC/CI not answering")
			},
		},
		{
			name: "laptop panel is skipped even if it answers DDC",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:00:02.0", driver: "i915", card: "card0", buses: []int{4}}},
				connectors: []fxConnector{
					{name: "card0-eDP-1", gpu: 0, status: "connected", edid: edidA, ddcBus: 4},
				},
				buses: map[int]fxBus{4: {edid: edidA, ddc: true}},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(len(res.monitors), 0)

				r := reportFor(is, res, 4)
				is.True(r.Panel)
				is.Equal(r.Probed, false)
				is.Equal(r.Result, "internal panel, no DDC/CI")
			},
		},
		{
			name: "MST phantom bus that doesn't answer DDC does not steal the connector",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:01:00.0", driver: "nvidia", card: "card1", buses: []int{4, 5}}},
				connectors: []fxConnector{
					{name: "card1-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: -1},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					5: {edid: edidA, ddc: false},
				},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4})

				m := res.monitors[0]
				is.True(m.DRMConnector != nil && *m.DRMConnector == "card1-DP-2")
			},
		},
		{
			name: "identical EDIDs on two connectors leave both monitors unlinked",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:01:00.0", driver: "nvidia", card: "card1", buses: []int{4, 5}}},
				connectors: []fxConnector{
					{name: "card1-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: -1},
					{name: "card1-DP-3", gpu: 0, status: "connected", edid: edidA, ddcBus: -1},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					5: {edid: edidA, ddc: true},
				},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4, 5})

				for _, m := range res.monitors {
					is.True(m.DRMConnector == nil)
				}

				is.Equal(len(res.Unlinked), 2)
			},
		},
		{
			name: "stale sysfs EDID ignores the direct link",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}}},
				connectors: []fxConnector{
					{name: "card0-DP-2", gpu: 0, status: "connected", edid: edidOld, ddcBus: 4},
				},
				buses: map[int]fxBus{4: {edid: edidB, ddc: true}},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4})

				r := reportFor(is, res, 4)
				is.Equal(r.Connector, "")
				is.Equal(r.Sysfs, "mismatch")
				is.Equal(res.Unlinked, []string{"card0-DP-2"})
			},
		},
		{
			name: "no DRM information falls back to every I2C bus",
			fx: fixture{
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					6: {edid: nil},
				},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4})

				r := reportFor(is, res, 6)
				is.True(strings.HasPrefix(r.Result, "no EDID"))
			},
		},
		{
			name: "SMBus adapters are not probed",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4, 7}}},
				connectors: []fxConnector{
					{name: "card0-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: 4},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					7: {edid: edidB, ddc: true, smbus: true},
				},
			},
			check: func(is *is.I, res ScanResult, reads []int) {
				is.True(!slices.Contains(reads, 7))
				is.Equal(monitorBuses(res), []int{4})
			},
		},
		{
			name: "SMBus adapters are skipped without DRM information",
			fx: fixture{
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					7: {edid: edidB, ddc: true, smbus: true},
				},
			},
			check: func(is *is.I, res ScanResult, reads []int) {
				is.True(!slices.Contains(reads, 7))
				is.Equal(monitorBuses(res), []int{4})
			},
		},
		{
			name: "GPU with no attached display is not probed",
			fx: fixture{
				gpus: []fxGPU{
					{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4}},
					{pci: "0000:04:00.0", driver: "amdgpu", card: "card1", buses: []int{6}},
				},
				connectors: []fxConnector{
					{name: "card0-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: 4},
					{name: "card1-DP-1", gpu: 1, status: "disconnected", edid: edidB, ddcBus: -1},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					6: {edid: edidB, ddc: true},
				},
			},
			check: func(is *is.I, res ScanResult, reads []int) {
				is.True(!slices.Contains(reads, 6))
				is.Equal(monitorBuses(res), []int{4})
				is.Equal(len(res.Unlinked), 0)
			},
		},
		{
			name: "EDID that can't be parsed is reported",
			fx: fixture{
				buses: map[int]fxBus{4: {edid: edidBad[:], ddc: true}},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(len(res.monitors), 0)
				is.Equal(reportFor(is, res, 4).Result, "EDID could not be parsed")
			},
		},
		{
			name: "EDID twin does not take a connector already linked by DDC",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4, 5}}},
				connectors: []fxConnector{
					{name: "card0-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: 4},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					5: {edid: edidA, ddc: true},
				},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4, 5})

				r4 := reportFor(is, res, 4)
				is.Equal(r4.Connector, "card0-DP-2")
				is.Equal(r4.Link, "ddc")

				r5 := reportFor(is, res, 5)
				is.Equal(r5.Connector, "")
				is.Equal(r5.Link, "")
				is.Equal(r5.Result, "monitor")

				is.Equal(len(res.Unlinked), 0)
			},
		},
		{
			name: "two DDC monitors with one EDID-matching connector are both left unlinked",
			fx: fixture{
				gpus: []fxGPU{{pci: "0000:03:00.0", driver: "amdgpu", card: "card0", buses: []int{4, 5}}},
				connectors: []fxConnector{
					{name: "card0-DP-2", gpu: 0, status: "connected", edid: edidA, ddcBus: -1},
				},
				buses: map[int]fxBus{
					4: {edid: edidA, ddc: true},
					5: {edid: edidA, ddc: true},
				},
			},
			check: func(is *is.I, res ScanResult, _ []int) {
				is.Equal(monitorBuses(res), []int{4, 5})

				for _, bus := range []int{4, 5} {
					r := reportFor(is, res, bus)
					is.Equal(r.Connector, "")
					is.Equal(r.Result, "monitor, connector ambiguous")
				}

				is.Equal(res.Unlinked, []string{"card0-DP-2"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			host, reads := installFixture(t, tt.fx)
			res := host.Scan()
			tt.check(is, res, *reads)
		})
	}
}

func TestScanWithoutDRMPath(t *testing.T) {
	is := is.New(t)

	host, reads := installFixture(t, fixture{
		buses: map[int]fxBus{4: {edid: makeEDID("ALPHA"), ddc: true}},
	})
	host.DRMPath = filepath.Join(t.TempDir(), "missing")

	res := host.Scan()
	is.Equal(monitorBuses(res), []int{4})
	is.Equal(*reads, []int{4})
}

func TestSysfsEDIDAgrees(t *testing.T) {
	monitorEDID := edidWith(1)
	m := Monitor{I2CBus: 4, EDIDRaw: monitorEDID}

	same := slices.Clone(monitorEDID[:])

	other := edidWith(2)

	tests := []struct {
		name      string
		connector drmConnector
		want      bool
	}{
		{"same EDID", drmConnector{edid: same}, true},
		{"same EDID with extension block", drmConnector{edid: append(slices.Clone(same), 0xAA, 0xBB)}, true},
		{"different EDID (stale link)", drmConnector{edid: other[:]}, false},
		{"no sysfs EDID", drmConnector{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(sysfsEDIDAgrees(tt.connector, m), tt.want)
		})
	}
}

func TestResolveConnectorConflicts(t *testing.T) {
	is := is.New(t)

	name := "card1-DP-3"
	other := "card1-DP-2"

	monitors := []Monitor{
		{I2CBus: 4, DRMConnector: &name},
		{I2CBus: 5, DRMConnector: &name},
		{I2CBus: 6, DRMConnector: &other},
	}

	resolveConnectorConflicts(monitors)

	is.True(monitors[0].DRMConnector == nil)
	is.True(monitors[1].DRMConnector == nil)
	is.True(monitors[2].DRMConnector != nil && *monitors[2].DRMConnector == other)
}

func TestIsInternalPanel(t *testing.T) {
	tests := []struct {
		connector string
		want      bool
	}{
		{"card1-eDP-1", true},
		{"card0-LVDS-1", true},
		{"card1-DP-3", false},
		{"card0-HDMI-A-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.connector, func(t *testing.T) {
			is := is.New(t)

			is.Equal(isInternalPanel(tt.connector), tt.want)
		})
	}
}

func TestMayBeConnected(t *testing.T) {
	tests := []struct {
		name      string
		connector drmConnector
		want      bool
	}{
		{"connected, reliable status", drmConnector{connected: true, statusReliable: true}, true},
		{"disconnected, reliable status", drmConnector{connected: false, statusReliable: true}, false},
		{"disconnected, unreliable status", drmConnector{connected: false, statusReliable: false}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(tt.connector.mayBeConnected(), tt.want)
		})
	}
}
