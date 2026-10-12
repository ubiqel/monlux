package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DRMConnector is a connected or connectable output of a GPU, as seen in sysfs.
type DRMConnector struct {
	Name string
	Bus  *int
	EDID []byte
	// Connected is the connector's sysfs status. It's only meaningful when
	// StatusReliable is true.
	Connected      bool
	StatusReliable bool
}

// mayBeConnected reports whether a monitor could be attached to the connector.
// Connectors whose status can't be trusted are always considered.
func (c DRMConnector) mayBeConnected() bool {
	return c.Connected || !c.StatusReliable
}

// candidateBuses returns the I2C buses worth probing for a DDC monitor.
//
// A connector with an explicit "ddc" symlink names its bus directly. Some
// drivers (notably NVIDIA) do not expose that link, so as a fallback we take
// every I2C adapter belonging to a GPU that has a connected connector. This
// keeps us from probing GPUs with no attached display, whose DDC buses can
// block for hundreds of milliseconds waiting on an absent monitor.
func (h Host) candidateBuses(connectors []DRMConnector) []int {
	set := make(map[int]struct{})

	for _, connector := range connectors {
		if !connector.mayBeConnected() {
			continue
		}

		if connector.Bus != nil {
			set[*connector.Bus] = struct{}{}
		}

		for _, bus := range h.gpuI2CBuses(connector.Name) {
			set[bus] = struct{}{}
		}
	}

	buses := make([]int, 0, len(set))
	for bus := range set {
		buses = append(buses, bus)
	}

	return buses
}

// gpuI2CBuses returns the I2C adapters attached to the GPU that owns the
// given DRM connector.
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

// allI2CBuses returns every i2c-* bus device.
func (h Host) allI2CBuses() []int {
	busPaths, _ := filepath.Glob(filepath.Join(h.DevPath, "i2c-*"))

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

// gpuDevicePath returns the PCI device of the GPU that owns a DRM connector.
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

// connectorDriver returns the kernel driver bound to the GPU that owns the
// connector (e.g. "amdgpu" or "nvidia"), or "" if it can't be determined.
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
	name, err := os.ReadFile(filepath.Join(h.I2CSysPath, fmt.Sprintf("i2c-%d", bus), "name"))
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToLower(string(name)), "smbus")
}

func (h Host) findDRMConnectors() ([]DRMConnector, error) {
	entries, err := os.ReadDir(h.DRMPath)
	if err != nil {
		return nil, fmt.Errorf("reading DRM path: %w", err)
	}

	connectors := make([]DRMConnector, 0)
	for _, entry := range entries {
		name := entry.Name()

		// Connectors are named like "card1-DP-3". The bare "card1" entry is
		// the GPU itself.
		if !strings.HasPrefix(name, "card") || !strings.Contains(name, "-") {
			continue
		}

		connectorPath := filepath.Join(h.DRMPath, name)

		// A DRM connector has a "status" attribute.
		status, err := os.ReadFile(filepath.Join(connectorPath, "status"))
		if err != nil {
			continue
		}

		connector := DRMConnector{
			Name:      name,
			Connected: strings.TrimSpace(string(status)) == "connected",
			// NVIDIA's proprietary driver sometimes reports "disconnected",
			// so its status is ignored.
			StatusReliable: h.connectorDriver(name) != "nvidia",
		}

		// First try the kernel's direct connector -> I2C mapping.
		ddcPath := filepath.Join(connectorPath, "ddc")

		target, err := filepath.EvalSymlinks(ddcPath)
		if err == nil {
			if bus, ok := busFromPath(target); ok {
				connector.Bus = &bus
			} else {
				slog.Debug("Couldn't get I2C bus from path", "path", target)
			}
		}

		// If there is no direct I2C mapping, keep the connector anyway.
		// Its EDID may allow us to associate it with an I2C monitor later.
		edid, err := os.ReadFile(filepath.Join(connectorPath, "edid"))
		if err == nil && len(edid) > 0 {
			connector.EDID = edid
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
