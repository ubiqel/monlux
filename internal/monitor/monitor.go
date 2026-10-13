// Package monitor finds the DDC/CI monitors attached to the machine and selects
// one by its monitor identifier.
package monitor

import (
	"fmt"
	"log/slog"

	"github.com/ubiqel/monlux/internal/i2c"
	"golang.org/x/sys/unix"
)

// Host is everything the package reaches outside the process: the sysfs and
// /dev trees and the DDC/CI hardware. DefaultHost describes the real machine.
// Tests build a Host that points at a fixture tree and fakes the hardware.
type Host struct {
	DRMPath    string // DRM connectors, usually /sys/class/drm
	I2CSysPath string // I2C adapters, usually /sys/bus/i2c/devices
	DevPath    string // device nodes, usually /dev

	ReadEDID func(bus int) ([128]byte, error)
	ProbeDDC func(bus int) bool

	NoCache bool // scan every time and don't read or write the monitors cache
}

// DefaultHost returns a Host for the real machine.
func DefaultHost() Host {
	return Host{
		DRMPath:    "/sys/class/drm",
		I2CSysPath: "/sys/bus/i2c/devices",
		DevPath:    "/dev",
		ReadEDID:   readEDIDFromBus,
		ProbeDDC:   probeDDCOnBus,
	}
}

type Monitor struct {
	I2CBus       int       `json:"i2c_bus"`
	EDIDIdentity string    `json:"edid_identity"`
	EDIDRaw      [128]byte `json:"edid_raw"`
	DRMConnector *string   `json:"drm_connector,omitempty"`
}

// BusReport records what a scan found on one candidate I2C bus. There is one
// per candidate bus, so a bus that isn't reported as a monitor can be explained.
type BusReport struct {
	Bus       int
	Identity  string // EDID identity; empty if the EDID couldn't be read
	Connector string // linked DRM connector; empty if none
	Link      string // how Connector was linked: "ddc" or "edid"
	Sysfs     string // sysfs EDID vs the bus EDID: "match", "mismatch", or "none"
	Panel     bool   // the linked connector is a laptop's built-in panel
	Probed    bool   // a DDC/CI probe was attempted
	DDC       bool   // the bus answered DDC/CI (only meaningful when Probed)
	Result    string // "monitor", or why the bus isn't reported as one
}

// ScanResult is the outcome of one scan.
type ScanResult struct {
	Monitors []Monitor
	Buses    []BusReport
	// Unlinked lists connected connectors that no monitor was linked to.
	Unlinked []string
}

func readEDIDFromBus(bus int) ([128]byte, error) {
	fd, err := i2c.Open(bus)
	if err != nil {
		return [128]byte{}, fmt.Errorf("opening bus: %w", err)
	}
	defer unix.Close(fd)

	edid, err := i2c.ReadEdid(fd)
	if err != nil {
		return [128]byte{}, fmt.Errorf("reading EDID: %w", err)
	}

	return edid, nil
}

func probeDDCOnBus(bus int) bool {
	fd, err := i2c.Open(bus)
	if err != nil {
		slog.Debug("Failed to open fd for I2C bus.", "error", err, "bus", bus)

		return false
	}
	defer unix.Close(fd)

	return i2c.ProbeDDC(fd)
}
