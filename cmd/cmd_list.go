package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

const verboseFlag = "verbose"

func runListCmd(ctx context.Context, cmd *cli.Command) error {
	lck, err := lock.Acquire()
	if err != nil {
		return fmt.Errorf("acquiring lock: %w\n", err)
	}
	defer lck.Release()

	monitors := scanMonitors()

	type row struct {
		drm        string
		i2c        string
		brightness string
		edid       string
		bus        int
	}

	// TODO: don't like all this shit. refactor
	//
	// We make parallel requests here to get the brightness, but they may still
	// end up sequential (lock inside driver). For example, it happens on
	// Nvidia card with two monitors connected. On my machine sequential and
	// parallel implementation perform the same so i've kept parallel version.
	wg := sync.WaitGroup{}
	rows := make([]row, len(monitors))
	for i, m := range monitors {
		wg.Add(1)
		go func() {
			defer wg.Done()

			r := row{
				drm:  *m.DRMConnector,
				i2c:  fmt.Sprintf("/dev/i2c-%d", m.I2CBus),
				edid: m.EDIDIdentity,
				bus:  m.I2CBus,
			}

			fd, err := openBus(m.I2CBus)
			if err != nil {
				slog.Warn("Failed to open fd for I2C bus", "bus", m.I2CBus)
				r.brightness = "?"
				rows[i] = r

				return
			}

			pct, _, _, err := i2c.ReadBrightness(fd)
			unix.Close(fd)
			if err == nil {
				r.brightness = fmt.Sprintf("%d%%", pct)
			} else {
				r.brightness = "?"
			}
			rows[i] = r
		}()
	}
	wg.Wait()

	if cmd.Bool(verboseFlag) {
		for i, r := range rows {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("Display %d\n", i+1)
			fmt.Printf("   I2C bus:       %s\n", r.i2c)
			fmt.Printf("   DRM connector: %s\n", r.drm)
			fmt.Printf("   EDID:          %s\n", r.edid)
			fmt.Printf("   Brightness:    %s\n", r.brightness)
		}

		return nil
	}

	// Compact table output with dynamic column widths
	headers := []string{"DRM", "EDID", "BRIGHTNESS"}
	colWidths := []int{len(headers[0]), len(headers[1]), len(headers[2])}
	for _, r := range rows {
		if w := len(r.drm); w > colWidths[0] {
			colWidths[0] = w
		}
		if w := len(r.edid); w > colWidths[1] {
			colWidths[1] = w
		}
		if w := len(r.brightness); w > colWidths[2] {
			colWidths[2] = w
		}
	}

	fmt.Printf("%-*s  %-*s  %s\n",
		colWidths[0], headers[0],
		colWidths[1], headers[1],
		headers[2])

	for _, r := range rows {
		fmt.Printf("%-*s  %-*s  %s\n",
			colWidths[0], r.drm,
			colWidths[1], r.edid, r.brightness)
	}

	return nil
}

type Monitor struct {
	I2CBus       int
	EDIDIdentity string
	EDIDRaw      [128]byte
	DRMConnector *string
}

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

func scanMonitors() []Monitor {
	// TODO: cache

	// Discover DRM connectors first. Their status (when trusted) and (when
	// available) DDC/I2C mapping tell us which buses are worth probing, so we
	// avoid touching buses whose connectors are disconnected. Reads on those
	// can block for hundreds of milliseconds waiting on an absent monitor.
	connectors, err := findDRMConnectors()
	if err != nil {
		slog.Debug("Failed to find DRM connectors.", "error", err)
		connectors = nil
	}

	buses := candidateBuses(connectors)
	if len(buses) == 0 {
		// Without usable DRM information, fall back to probing every bus.
		buses = allI2CBuses()
	}
	slices.Sort(buses)

	// Probe EDID for each candidate bus.
	monitors := make([]Monitor, 0, len(buses))
	for _, bus := range buses {
		fd, err := openBus(bus)
		if err != nil {
			slog.Debug("Failed to open fd for I2C bus.", "error", err, "bus", bus)

			continue
		}

		edid, err := i2c.ReadEdid(fd)
		unix.Close(fd)
		if err != nil {
			slog.Debug("Failed to read EDID from I2C bus.", "error", err, "bus", bus)

			continue
		}

		identity, ok := i2c.ParseIdentity(edid[:])
		if !ok {
			slog.Debug("Failed to parse identity from EDID.", "bus", bus)

			continue
		}

		monitors = append(monitors, Monitor{
			I2CBus:       bus,
			EDIDIdentity: identity,
			EDIDRaw:      edid,
		})
	}

	if len(connectors) == 0 {
		return filterDDCMonitors(monitors)
	}

	// First, associate connectors through the kernel's direct
	// connector -> DDC/I2C mapping.
	for i := range monitors {
		for _, connector := range connectors {
			if !connector.mayBeConnected() || connector.Bus == nil {
				continue
			}

			if *connector.Bus == monitors[i].I2CBus {
				name := connector.Name
				monitors[i].DRMConnector = &name
				break
			}
		}
	}

	// Then try EDID matching for monitors that weren't associated
	// through the direct DDC/I2C mapping.
	for i, monitor := range monitors {
		if monitor.DRMConnector != nil {
			continue
		}

		var match *DRMConnector
		matches := 0

		for _, connector := range connectors {
			if !connector.mayBeConnected() {
				continue
			}

			if len(connector.EDID) < 128 { // to avoid panic
				continue
			}

			// EDID length can be bigger than 128 bytes (due to extension
			// blocks) so we cut the size.
			if !bytes.Equal(monitor.EDIDRaw[:], connector.EDID[:128]) {
				continue
			}

			match = &connector
			matches++
		}

		// Exactly one EDID match is safe.
		if matches == 1 {
			name := match.Name
			monitors[i].DRMConnector = &name
		}

		// 0 matches: DRM connector remains unknown.
		// >1 matches: ambiguous, so leave it unknown.
	}

	return filterDDCMonitors(monitors)
}

// candidateBuses returns the I2C buses worth probing for a DDC monitor.
//
// A connector with an explicit "ddc" symlink names its bus directly. Some
// drivers (notably NVIDIA) do not expose that link, so as a fallback we take
// every I2C adapter belonging to a GPU that has a connected connector. This
// keeps us from probing GPUs with no attached display, whose DDC buses can
// block for hundreds of milliseconds waiting on an absent monitor.
func candidateBuses(connectors []DRMConnector) []int {
	set := make(map[int]struct{})

	for _, connector := range connectors {
		if !connector.mayBeConnected() {
			continue
		}

		if connector.Bus != nil {
			set[*connector.Bus] = struct{}{}
		}

		for _, bus := range gpuI2CBuses(connector.Name) {
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
func gpuI2CBuses(connectorName string) []int {
	gpuPath, err := gpuDevicePath(connectorName)
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
			if isSMBusAdapter(bus) {
				continue
			}

			buses = append(buses, bus)
		} else {
			slog.Debug("Couldn't get I2C bus from path", "path", match)
		}
	}

	return buses
}

// allI2CBuses returns every /dev/i2c-* bus.
func allI2CBuses() []int {
	busPaths, _ := filepath.Glob("/dev/i2c-*")

	buses := make([]int, 0, len(busPaths))
	for _, busPath := range busPaths {
		num, ok := busFromPath(busPath)
		if !ok {
			slog.Debug("Couldn't get I2C bus from path", "path", busPath)

			continue
		}

		if isSMBusAdapter(num) {
			continue
		}

		buses = append(buses, num)
	}

	return buses
}

const drmPath = "/sys/class/drm"

// isInternalPanel reports whether a connector is a laptop's built-in panel.
// Panels expose EDID over I2C but never answer DDC/CI.
func isInternalPanel(connectorName string) bool {
	return strings.Contains(connectorName, "-eDP-") || strings.Contains(connectorName, "-LVDS-")
}

// gpuDevicePath returns the PCI device of the GPU that owns a DRM connector.
func gpuDevicePath(connectorName string) (string, error) {
	devicePath := filepath.Join(drmPath, connectorName, "device")

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
func connectorDriver(connectorName string) string {
	gpuPath, err := gpuDevicePath(connectorName)
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
func isSMBusAdapter(bus int) bool {
	name, err := os.ReadFile(fmt.Sprintf("/sys/bus/i2c/devices/i2c-%d/name", bus))
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToLower(string(name)), "smbus")
}

// filterDDCMonitors keeps only monitors that answer DDC/CI. EDID alone isn't
// enough: laptop panels answer EDID but not DDC/CI.
func filterDDCMonitors(monitors []Monitor) []Monitor {
	result := monitors[:0]
	for _, m := range monitors {
		if supportsDDC(m) {
			result = append(result, m)
		}
	}

	return result
}

func supportsDDC(m Monitor) bool {
	if m.DRMConnector != nil && isInternalPanel(*m.DRMConnector) {
		slog.Debug("Skipping internal panel, it doesn't support DDC/CI.", "connector", *m.DRMConnector)

		return false
	}

	fd, err := openBus(m.I2CBus)
	if err != nil {
		slog.Debug("Failed to open fd for I2C bus.", "error", err, "bus", m.I2CBus)

		return false
	}
	defer unix.Close(fd)

	if !i2c.ProbeDDC(fd) {
		slog.Debug("Bus answers EDID but not DDC/CI.", "bus", m.I2CBus)

		return false
	}

	return true
}

func findDRMConnectors() ([]DRMConnector, error) {
	entries, err := os.ReadDir(drmPath)
	if err != nil {
		return nil, err
	}

	connectors := make([]DRMConnector, 0)
	for _, entry := range entries {
		name := entry.Name()

		// Connectors are named like "card1-DP-3". The bare "card1" entry is
		// the GPU itself.
		if !strings.HasPrefix(name, "card") || !strings.Contains(name, "-") {
			continue
		}

		connectorPath := filepath.Join(drmPath, name)

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
			StatusReliable: connectorDriver(name) != "nvidia",
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
