package monitor

import (
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
				is.Equal(len(res.Monitors), 0)

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
				is.Equal(len(res.Monitors), 0)

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

				m := res.Monitors[0]
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

				for _, m := range res.Monitors {
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

func TestSysfsEDIDAgrees(t *testing.T) {
	monitorEDID := edidWith(1)
	m := Monitor{I2CBus: 4, EDIDRaw: monitorEDID}

	same := make([]byte, 128)
	copy(same, monitorEDID[:])

	other := edidWith(2)

	tests := []struct {
		name      string
		connector DRMConnector
		want      bool
	}{
		{"same EDID", DRMConnector{EDID: same}, true},
		{"same EDID with extension block", DRMConnector{EDID: append(same, 0xAA, 0xBB)}, true},
		{"different EDID (stale link)", DRMConnector{EDID: other[:]}, false},
		{"no sysfs EDID", DRMConnector{}, true},
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
		connector DRMConnector
		want      bool
	}{
		{"connected, reliable status", DRMConnector{Connected: true, StatusReliable: true}, true},
		{"disconnected, reliable status", DRMConnector{Connected: false, StatusReliable: true}, false},
		{"disconnected, unreliable status", DRMConnector{Connected: false, StatusReliable: false}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(tt.connector.mayBeConnected(), tt.want)
		})
	}
}
