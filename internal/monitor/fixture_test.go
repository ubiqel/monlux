package monitor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/matryer/is"
)

type fxGPU struct {
	pci    string
	driver string
	card   string
	buses  []int // each must also be in fixture.buses
}

type fxConnector struct {
	name   string
	gpu    int
	status string
	edid   []byte // nil leaves the file empty
	ddcBus int    // -1 for no direct "ddc" link
}

type fxBus struct {
	edid  []byte // nil makes the EDID read fail
	ddc   bool
	smbus bool
}

type fixture struct {
	gpus       []fxGPU
	connectors []fxConnector
	buses      map[int]fxBus
}

func makeEDID(name string) []byte {
	e := make([]byte, 128)
	copy(e, []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})

	// Manufacturer "GBT" packed as three 5-bit letters.
	mfg := uint16('G'-'A'+1)<<10 | uint16('B'-'A'+1)<<5 | uint16('T'-'A'+1)
	e[8], e[9] = byte(mfg>>8), byte(mfg)
	e[10], e[11] = 0x34, 0x12

	e[54+3] = 0xFC
	copy(e[54+5:], name+"\n")

	return e
}

// The returned slice records every bus whose EDID was read.
func installFixture(t *testing.T, fx fixture) (Host, *[]int) {
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

	host := Host{
		DRMPath:    drm,
		i2cSysPath: i2cSys,
		devPath:    dev,
		readEDID: func(bus int) ([128]byte, error) {
			edidReads = append(edidReads, bus)

			b, ok := fx.buses[bus]
			if !ok || b.edid == nil {
				return [128]byte{}, errors.New("i2c_rdwr: input/output error")
			}

			var e [128]byte
			copy(e[:], b.edid)

			return e, nil
		},
		probeDDC: func(bus int) bool {
			return fx.buses[bus].ddc
		},
	}

	return host, &edidReads
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

func reportFor(is *is.I, res ScanResult, bus int) BusReport {
	for _, r := range res.Buses {
		if r.Bus == bus {
			return r
		}
	}

	is.Fail() // no report for this bus

	return BusReport{}
}

func monitorBuses(res ScanResult) []int {
	buses := make([]int, 0, len(res.monitors))
	for _, m := range res.monitors {
		buses = append(buses, m.I2CBus)
	}
	slices.Sort(buses)

	return buses
}
