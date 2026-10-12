package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/matryer/is"
)

// fxGPU is a GPU in a fixture tree.
type fxGPU struct {
	pci    string // PCI address, e.g. "0000:01:00.0"
	driver string // bound driver, e.g. "nvidia" or "amdgpu"
	card   string // DRM card name, e.g. "card1"
	buses  []int  // I2C adapters under this GPU; each must also be in fixture.buses
}

// fxConnector is a DRM connector in a fixture tree.
type fxConnector struct {
	name   string // e.g. "card1-DP-3"
	gpu    int    // index into fixture.gpus
	status string // sysfs status: "connected" or "disconnected"
	edid   []byte // sysfs edid; nil leaves the file empty
	ddcBus int    // direct "ddc" link to this bus; -1 for none
}

// fxBus is an I2C bus in a fixture tree.
type fxBus struct {
	edid  []byte // EDID the bus returns; nil makes the EDID read fail
	ddc   bool   // whether DDC/CI answers on this bus
	smbus bool   // adapter is named as SMBus
}

type fixture struct {
	gpus       []fxGPU
	connectors []fxConnector
	buses      map[int]fxBus
}

// makeEDID builds a minimal 128-byte EDID with the given name descriptor.
// Different names give different EDIDs.
func makeEDID(name string) []byte {
	e := make([]byte, 128)
	copy(e, []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})

	// Manufacturer "GBT" packed as three 5-bit letters.
	mfg := uint16('G'-'A'+1)<<10 | uint16('B'-'A'+1)<<5 | uint16('T'-'A'+1)
	e[8], e[9] = byte(mfg>>8), byte(mfg)
	e[10], e[11] = 0x34, 0x12 // product code

	e[54+3] = 0xFC // monitor name descriptor
	copy(e[54+5:], name+"\n")

	return e
}

// installFixture builds a fake sysfs and /dev tree in a temp directory, points
// the scanner at it, and replaces hardware access with the fake buses. The
// returned slice records every bus whose EDID was read.
func installFixture(t *testing.T, fx fixture) *[]int {
	t.Helper()

	root := t.TempDir()
	drm := filepath.Join(root, "sys/class/drm")
	i2cSys := filepath.Join(root, "sys/bus/i2c/devices")
	dev := filepath.Join(root, "dev")
	pci := filepath.Join(root, "sys/devices/pci")
	drivers := filepath.Join(root, "sys/bus/pci/drivers")

	for _, d := range []string{drm, i2cSys, dev, pci, drivers} {
		mustMkdir(t, d)
	}

	oldDRM, oldI2C, oldDev := drmPath, i2cSysPath, devPath
	oldRead, oldProbe := readEDID, probeDDC
	t.Cleanup(func() {
		drmPath, i2cSysPath, devPath = oldDRM, oldI2C, oldDev
		readEDID, probeDDC = oldRead, oldProbe
	})
	drmPath, i2cSysPath, devPath = drm, i2cSys, dev

	gpuDirs := make([]string, len(fx.gpus))
	for i, g := range fx.gpus {
		gpuDir := filepath.Join(pci, g.pci)
		gpuDirs[i] = gpuDir

		mustMkdir(t, filepath.Join(gpuDir, "drm", g.card))
		mustMkdir(t, filepath.Join(drm, g.card)) // the GPU's own entry, which has no status

		drvDir := filepath.Join(drivers, g.driver)
		mustMkdir(t, drvDir)
		mustSymlink(t, drvDir, filepath.Join(gpuDir, "driver"))

		for _, bus := range g.buses {
			mustMkdir(t, filepath.Join(gpuDir, fmt.Sprintf("i2c-%d", bus)))
		}
	}

	for bus, b := range fx.buses {
		adapter := filepath.Join(i2cSys, fmt.Sprintf("i2c-%d", bus))
		mustMkdir(t, adapter)

		name := "AMDGPU DM i2c hw bus\n"
		if b.smbus {
			name = "SMBus I801 adapter\n"
		}
		mustWrite(t, filepath.Join(adapter, "name"), []byte(name))
		mustWrite(t, filepath.Join(dev, fmt.Sprintf("i2c-%d", bus)), nil)
	}

	for _, c := range fx.connectors {
		dir := filepath.Join(drm, c.name)
		mustMkdir(t, dir)
		mustWrite(t, filepath.Join(dir, "status"), []byte(c.status+"\n"))
		mustWrite(t, filepath.Join(dir, "edid"), c.edid)

		g := fx.gpus[c.gpu]
		mustSymlink(t, filepath.Join(gpuDirs[c.gpu], "drm", g.card), filepath.Join(dir, "device"))

		if c.ddcBus >= 0 {
			mustSymlink(t, filepath.Join(i2cSys, fmt.Sprintf("i2c-%d", c.ddcBus)), filepath.Join(dir, "ddc"))
		}
	}

	var edidReads []int

	readEDID = func(bus int) ([128]byte, error) {
		edidReads = append(edidReads, bus)

		b, ok := fx.buses[bus]
		if !ok || b.edid == nil {
			return [128]byte{}, errors.New("i2c_rdwr: input/output error")
		}

		var e [128]byte
		copy(e[:], b.edid)

		return e, nil
	}

	probeDDC = func(bus int) bool {
		return fx.buses[bus].ddc
	}

	return &edidReads
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()

	is := is.New(t)
	is.NoErr(os.MkdirAll(path, 0o755))
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()

	is := is.New(t)
	is.NoErr(os.WriteFile(path, data, 0o644))
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()

	is := is.New(t)
	is.NoErr(os.Symlink(target, link))
}

// reportFor returns the report for a bus, failing the test if there is none.
func reportFor(is *is.I, res ScanResult, bus int) BusReport {
	for _, r := range res.Buses {
		if r.Bus == bus {
			return r
		}
	}

	is.Fail() // no report for this bus

	return BusReport{}
}

// monitorBuses returns the buses of the monitors found, sorted.
func monitorBuses(res ScanResult) []int {
	buses := make([]int, 0, len(res.Monitors))
	for _, m := range res.Monitors {
		buses = append(buses, m.I2CBus)
	}
	slices.Sort(buses)

	return buses
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

			reads := installFixture(t, tt.fx)
			res := scan()
			tt.check(is, res, *reads)
		})
	}
}
