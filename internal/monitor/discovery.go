package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type drmConnector struct {
	name string
	bus  *int
	edid []byte
	// connected is only meaningful when StatusReliable is true.
	connected      bool
	statusReliable bool
}

func (c drmConnector) mayBeConnected() bool {
	return c.connected || !c.statusReliable
}

// candidateBuses returns the I2C buses worth probing for a DDC monitor.
//
// A connector with an explicit "ddc" symlink names its bus directly. Some
// drivers (notably NVIDIA) do not expose that link, so as a fallback we take
// every I2C adapter belonging to a GPU that has a connected connector. This
// keeps us from probing GPUs with no attached display, whose DDC buses can
// block for hundreds of milliseconds waiting on an absent monitor.
func (h Host) candidateBuses(connectors []drmConnector) []int {
	set := make(map[int]struct{})

	for _, connector := range connectors {
		if !connector.mayBeConnected() {
			continue
		}

		if connector.bus != nil {
			set[*connector.bus] = struct{}{}
		}

		for _, bus := range h.gpuI2CBuses(connector.name) {
			set[bus] = struct{}{}
		}
	}

	buses := make([]int, 0, len(set))
	for bus := range set {
		buses = append(buses, bus)
	}

	return buses
}

func (h Host) gpuI2CBuses(connectorName string) []int {
	gpuPath, err := h.gpuDevicePath(connectorName)
	if err != nil {
		return nil
	}

	matches, err := filepath.Glob(filepath.Join(gpuPath, "i2c-*"))
	if err != nil {
		return nil
	}

	buses := make([]int, 0, len(matches))
	for _, match := range matches {
		if bus, ok := busFromPath(match); ok {
			if h.isSMBusAdapter(bus) {
				continue
			}

			buses = append(buses, bus)
		} else {
			slog.Debug("Couldn't get I2C bus from path", "path", match)
		}
	}

	return buses
}

func (h Host) allI2CBuses() []int {
	busPaths, _ := filepath.Glob(filepath.Join(h.devPath, "i2c-*"))

	buses := make([]int, 0, len(busPaths))
	for _, busPath := range busPaths {
		num, ok := busFromPath(busPath)
		if !ok {
			slog.Debug("Couldn't get I2C bus from path", "path", busPath)

			continue
		}

		if h.isSMBusAdapter(num) {
			continue
		}

		buses = append(buses, num)
	}

	return buses
}

func (h Host) gpuDevicePath(connectorName string) (string, error) {
	devicePath := filepath.Join(h.DRMPath, connectorName, "device")

	cardPath, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", devicePath, err)
	}

	// cardPath is ".../<gpu device>/drm/cardN", so the GPU device is two
	// levels up.
	return filepath.Dir(filepath.Dir(cardPath)), nil
}

func (h Host) connectorDriver(connectorName string) string {
	gpuPath, err := h.gpuDevicePath(connectorName)
	if err != nil {
		return ""
	}

	target, err := filepath.EvalSymlinks(filepath.Join(gpuPath, "driver"))
	if err != nil {
		return ""
	}

	return filepath.Base(target)
}

// isSMBusAdapter reports whether an I2C bus is an SMBus adapter. SMBus is not
// used for DDC/CI, so these buses are never worth probing.
func (h Host) isSMBusAdapter(bus int) bool {
	name, err := os.ReadFile(filepath.Join(h.i2cSysPath, fmt.Sprintf("i2c-%d", bus), "name"))
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToLower(string(name)), "smbus")
}

func (h Host) findDRMConnectors() ([]drmConnector, error) {
	entries, err := os.ReadDir(h.DRMPath)
	if err != nil {
		return nil, fmt.Errorf("reading DRM path: %w", err)
	}

	connectors := make([]drmConnector, 0)
	for _, entry := range entries {
		name := entry.Name()

		// Connectors are named like "card1-DP-3". The bare "card1" entry is
		// the GPU itself.
		if !strings.HasPrefix(name, "card") || !strings.Contains(name, "-") {
			continue
		}

		connectorPath := filepath.Join(h.DRMPath, name)

		status, err := os.ReadFile(filepath.Join(connectorPath, "status"))
		if err != nil {
			continue
		}

		connector := drmConnector{
			name:      name,
			connected: strings.TrimSpace(string(status)) == "connected",
			// NVIDIA's proprietary driver sometimes reports "disconnected",
			// so its status is ignored.
			statusReliable: h.connectorDriver(name) != "nvidia",
		}

		ddcPath := filepath.Join(connectorPath, "ddc")

		target, err := filepath.EvalSymlinks(ddcPath)
		if err == nil {
			if bus, ok := busFromPath(target); ok {
				connector.bus = &bus
			} else {
				slog.Debug("Couldn't get I2C bus from path", "path", target)
			}
		}

		// If there is no direct I2C mapping, keep the connector anyway.
		// Its EDID may allow us to associate it with an I2C monitor later.
		edid, err := os.ReadFile(filepath.Join(connectorPath, "edid"))
		if err == nil && len(edid) > 0 {
			connector.edid = edid
		}

		connectors = append(connectors, connector)
	}

	return connectors, nil
}

func busFromPath(path string) (int, bool) {
	name := filepath.Base(path)

	if !strings.HasPrefix(name, "i2c-") {
		return 0, false
	}

	num, err := strconv.Atoi(strings.TrimPrefix(name, "i2c-"))
	if err != nil {
		return 0, false
	}

	return num, true
}
