package monitor

import (
	"fmt"
	"log/slog"

	"github.com/ubiqel/monlux/internal/ddc"
)

// Host gives the package every path and hardware hook it uses outside the
// process, so tests can substitute a fixture tree and fake buses.
type Host struct {
	DRMPath    string
	I2CSysPath string
	DevPath    string

	ReadEDID func(bus int) ([128]byte, error)
	ProbeDDC func(bus int) bool
	OpenBus  func(bus int) (Bus, error)

	NoCache bool
}

type Bus interface {
	GetBrightness() (current, maximum uint16, err error)
	SetBrightness(value uint16) error
	Close() error
}

func DefaultHost() Host {
	return Host{
		DRMPath:    "/sys/class/drm",
		I2CSysPath: "/sys/bus/i2c/devices",
		DevPath:    "/dev",
		ReadEDID:   readEDIDFromBus,
		ProbeDDC:   probeDDCOnBus,
		OpenBus:    openDDCBus,
	}
}

// openDDCBus returns a nil interface on failure. Returning ddc.Open's result
// directly would wrap a nil *ddc.Bus in a non-nil Bus.
func openDDCBus(busNum int) (Bus, error) {
	bus, err := ddc.Open(busNum)
	if err != nil {
		return nil, fmt.Errorf("opening bus: %w", err)
	}

	return bus, nil
}

type Monitor struct {
	I2CBus       int       `json:"i2c_bus"`
	EDIDIdentity string    `json:"edid_identity"`
	EDIDRaw      [128]byte `json:"edid_raw"`
	DRMConnector *string   `json:"drm_connector,omitempty"`
}

// BusReport has one entry per candidate bus, so a bus that isn't reported as a
// monitor can still be explained.
type BusReport struct {
	Bus       int
	Identity  string
	Connector string
	Link      string
	Sysfs     string
	Panel     bool
	Probed    bool
	DDC       bool // only meaningful when Probed
	Result    string
}

type ScanResult struct {
	Monitors []Monitor
	Buses    []BusReport
	Unlinked []string
}

func readEDIDFromBus(busNum int) ([128]byte, error) {
	bus, err := ddc.Open(busNum)
	if err != nil {
		return [128]byte{}, fmt.Errorf("opening bus: %w", err)
	}
	defer bus.Close()

	edid, err := bus.ReadEDID()
	if err != nil {
		return [128]byte{}, fmt.Errorf("reading EDID: %w", err)
	}

	return edid, nil
}

func probeDDCOnBus(busNum int) bool {
	bus, err := ddc.Open(busNum)
	if err != nil {
		slog.Debug("Failed to open fd for I2C bus.", "error", err, "bus", busNum)

		return false
	}
	defer bus.Close()

	return bus.ProbeDDC()
}
