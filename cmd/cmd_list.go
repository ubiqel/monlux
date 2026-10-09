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

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

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
	var rows []row

	// get brightness
	for _, m := range monitors {
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
			rows = append(rows, r)

			continue
		}
		pct, _, _, err := i2c.ReadBrightness(fd)
		unix.Close(fd)
		if err == nil {
			r.brightness = fmt.Sprintf("%d%%", pct)
		} else {
			r.brightness = "?"
		}
		rows = append(rows, r)
	}

	if cmd.Bool(verboseFlag) {
		for i, r := range rows {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("Display %d\n", i+1)
			fmt.Printf("   I2C bus:       %s\n", r.i2c)
			fmt.Printf("   DRM connector: %s\n", r.drm)
			fmt.Printf("   Brightness:    %s\n", r.brightness)
			fmt.Printf("   EDID:          %s\n", r.edid)
		}

		return nil
	}

	// Compact table output with dynamic column widths
	headers := []string{"DRM", "BRIGHTNESS", "EDID"}
	colWidths := []int{len(headers[0]), len(headers[1]), len(headers[2])}
	for _, r := range rows {
		if w := len(r.drm); w > colWidths[0] {
			colWidths[0] = w
		}
		if w := len(r.brightness); w > colWidths[1] {
			colWidths[1] = w
		}
		if w := len(r.edid); w > colWidths[2] {
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
			colWidths[1], r.brightness,
			r.edid)
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
}

func scanMonitors() []Monitor {
	// TODO: cache

	// Discover connected DRM connectors first. Their status and (when
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
		return monitors
	}

	// First, associate connectors through the kernel's direct
	// connector -> DDC/I2C mapping.
	for i := range monitors {
		for _, connector := range connectors {
			if connector.Bus == nil {
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

	return monitors
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
	devicePath := filepath.Join(drmPath, connectorName, "device")

	cardPath, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return nil
	}

	// cardPath is ".../<gpu device>/drm/cardN", so the GPU device that owns
	// the I2C adapters is two levels up.
	gpuPath := filepath.Dir(filepath.Dir(cardPath))

	matches, err := filepath.Glob(filepath.Join(gpuPath, "i2c-*"))
	if err != nil {
		return nil
	}

	buses := make([]int, 0, len(matches))
	for _, match := range matches {
		if bus, ok := busFromPath(match); ok {
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

		buses = append(buses, num)
	}

	return buses
}

const drmPath = "/sys/class/drm"

func findDRMConnectors() ([]DRMConnector, error) {
	entries, err := os.ReadDir(drmPath)
	if err != nil {
		return nil, err
	}

	connectors := make([]DRMConnector, 0)
	for _, entry := range entries {
		name := entry.Name()

		if !strings.HasPrefix(name, "card") {
			continue
		}

		connectorPath := filepath.Join(drmPath, name)

		// A DRM connector has a "status" attribute.
		status, err := os.ReadFile(filepath.Join(connectorPath, "status"))
		if err != nil {
			continue
		}

		if strings.TrimSpace(string(status)) != "connected" {
			continue
		}

		connector := DRMConnector{
			Name: name,
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
