package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"golang.org/x/sys/unix"
)

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)

	var verbose bool
	fs.BoolVar(&verbose, "verbose", false, "show extended output")

	var help bool
	fs.BoolVar(&help, "help", false, "show this help and exit")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: shine list [options]\n\n")
		fmt.Fprintln(os.Stderr, "List monitors and their brightness.\n\n")
		fmt.Fprintln(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	if help {
		fs.Usage()
		return
	}

	lck, err := lock.Acquire()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to acquire lock: %s\n", err)
		os.Exit(1)
	}
	defer lock.Release(lck)

	monitors := scanMonitors()

	type row struct {
		drm        string
		i2c        string
		brightness string
		edid       string
		bus        int
	}
	var rows []row

	for _, m := range monitors {
		r := row{
			drm:  *m.DRMConnector,
			i2c:  fmt.Sprintf("/dev/i2c-%d", m.I2CBus),
			edid: m.EDIDIdentity,
			bus:  m.I2CBus,
		}

		fd, err := openBus(m.I2CBus)
		if err != nil {
			// TODO: think on proper logging (warning here)
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

	if verbose {
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
		return
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
	// Discover all /dev/i2c-*.
	busesPaths, _ := filepath.Glob("/dev/i2c-*")

	buses := make([]int, 0, len(busesPaths))
	for _, busPath := range busesPaths {
		num, ok := busFromPath(busPath)
		if !ok {
			// TODO: log
			continue
		}

		buses = append(buses, num)
	}
	slices.Sort(buses)

	// Probe EDID for each bus.
	monitors := make([]Monitor, 0, len(buses))

	for _, bus := range buses {
		fd, err := openBus(bus)
		if err != nil {
			// TODO: log
			continue
		}

		edid, err := i2c.ReadEdid(fd)
		unix.Close(fd)
		if err != nil {
			// TODO: log
			continue
		}

		identity, ok := i2c.ParseIdentity(edid[:])
		if !ok {
			// TODO: log
			continue
		}

		monitors = append(monitors, Monitor{
			I2CBus:       bus,
			EDIDIdentity: identity,
			EDIDRaw:      edid,
		})
	}

	// Discover connected DRM connectors.
	connectors, err := findDRMConnectors()
	if err != nil {
		// TODO: log
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
			if len(connector.EDID) == 0 {
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

func findDRMConnectors() ([]DRMConnector, error) {
	const drmPath = "/sys/class/drm"

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

func openBus(bus int) (fd int, err error) {
	return unix.Open(fmt.Sprintf("/dev/i2c-%d", bus), unix.O_RDWR, 0)
}
