package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/ubiqel/shine/internal/i2c"
	"golang.org/x/sys/unix"
)

// Hardware access, as variables so tests can replace it with fakes.
var (
	readEDID = readEDIDFromBus
	probeDDC = probeDDCOnBus
)

type Monitor struct {
	I2CBus       int
	EDIDIdentity string
	EDIDRaw      [128]byte
	DRMConnector *string
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

func scanMonitors() []Monitor {
	return scan().Monitors
}

func scan() ScanResult {
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

	var res ScanResult
	reportOf := make(map[int]int, len(buses)) // bus -> index in res.Buses

	// Read EDID for each candidate bus.
	var monitors []Monitor
	for _, bus := range buses {
		idx := len(res.Buses)
		reportOf[bus] = idx
		res.Buses = append(res.Buses, BusReport{Bus: bus})

		edid, err := readEDID(bus)
		if err != nil {
			slog.Debug("Failed to read EDID from I2C bus.", "error", err, "bus", bus)
			res.Buses[idx].Result = fmt.Sprintf("no EDID: %v", err)

			continue
		}

		identity, ok := i2c.ParseIdentity(edid[:])
		if !ok {
			slog.Debug("Failed to parse identity from EDID.", "bus", bus)
			res.Buses[idx].Result = "EDID could not be parsed"

			continue
		}

		res.Buses[idx].Identity = identity
		monitors = append(monitors, Monitor{
			I2CBus:       bus,
			EDIDIdentity: identity,
			EDIDRaw:      edid,
		})
	}

	links := associateConnectors(monitors, connectors)
	for _, m := range monitors {
		r := &res.Buses[reportOf[m.I2CBus]]
		r.Link = links[m.I2CBus]
		r.Sysfs = sysfsState(m, connectors)
		if m.DRMConnector != nil {
			r.Connector = *m.DRMConnector
		}
	}

	// Filter before resolving conflicts: a bus that doesn't answer DDC/CI
	// (such as a phantom MST entry) must not compete for a connector.
	var kept []Monitor
	for _, m := range monitors {
		r := &res.Buses[reportOf[m.I2CBus]]

		if m.DRMConnector != nil && isInternalPanel(*m.DRMConnector) {
			r.Panel = true
			r.Result = "internal panel, no DDC/CI"
			slog.Debug("Skipping internal panel, it doesn't support DDC/CI.", "connector", *m.DRMConnector)

			continue
		}

		r.Probed = true
		r.DDC = probeDDC(m.I2CBus)
		if !r.DDC {
			r.Result = "EDID only, DDC/CI not answering"
			slog.Debug("Bus answers EDID but not DDC/CI.", "bus", m.I2CBus)

			continue
		}

		kept = append(kept, m)
	}

	resolveConnectorConflicts(kept)
	for _, m := range kept {
		r := &res.Buses[reportOf[m.I2CBus]]
		r.Result = "monitor"

		if m.DRMConnector == nil && r.Connector != "" {
			r.Connector = ""
			r.Link = ""
			r.Result = "monitor, connector ambiguous"
		}
	}

	res.Unlinked = unlinkedConnectors(kept, connectors)
	for _, name := range res.Unlinked {
		slog.Debug("Connector has no DDC-capable monitor linked to it.", "connector", name)
	}

	warnDuplicateIdentities(kept)
	res.Monitors = kept

	return res
}

// associateConnectors links monitors to DRM connectors. The kernel's direct
// connector -> DDC/I2C mapping is tried first, then EDID matching for the
// rest. It sets DRMConnector on the monitors and returns how each bus was
// linked, keyed by bus.
func associateConnectors(monitors []Monitor, connectors []DRMConnector) map[int]string {
	links := make(map[int]string)

	// First, associate connectors through the kernel's direct
	// connector -> DDC/I2C mapping.
	for i := range monitors {
		for _, connector := range connectors {
			if !connector.mayBeConnected() || connector.Bus == nil {
				continue
			}

			if *connector.Bus != monitors[i].I2CBus {
				continue
			}

			// A stale or wrong direct link would attach the wrong monitor, so
			// it's only trusted when the connector's EDID agrees.
			if !sysfsEDIDAgrees(connector, monitors[i]) {
				slog.Debug("Direct DDC link disagrees with connector EDID, ignoring it.",
					"bus", monitors[i].I2CBus, "connector", connector.Name)

				break
			}

			name := connector.Name
			monitors[i].DRMConnector = &name
			links[monitors[i].I2CBus] = "ddc"

			break
		}
	}

	// Connectors already linked directly can't be claimed by EDID matching.
	claimed := make(map[string]bool)
	for _, m := range monitors {
		if m.DRMConnector != nil {
			claimed[*m.DRMConnector] = true
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
			if !connector.mayBeConnected() || claimed[connector.Name] {
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
			links[monitor.I2CBus] = "edid"
		}

		// 0 matches: DRM connector remains unknown.
		// >1 matches: ambiguous, so leave it unknown.
	}

	return links
}

// sysfsState describes how a monitor's EDID compares with sysfs. "match" means
// the monitor is linked to a connector. "mismatch" means the connector on the
// monitor's own bus reports a different EDID. "none" means neither applies.
func sysfsState(m Monitor, connectors []DRMConnector) string {
	if m.DRMConnector != nil {
		return "match"
	}

	for _, c := range connectors {
		if c.Bus != nil && *c.Bus == m.I2CBus && len(c.EDID) >= 128 {
			return "mismatch"
		}
	}

	return "none"
}

// unlinkedConnectors returns the names of connected connectors that no
// monitor is linked to. Internal panels are excluded, since they never have
// DDC/CI.
func unlinkedConnectors(monitors []Monitor, connectors []DRMConnector) []string {
	linked := make(map[string]bool)
	for _, m := range monitors {
		if m.DRMConnector != nil {
			linked[*m.DRMConnector] = true
		}
	}

	var names []string
	for _, c := range connectors {
		if !c.mayBeConnected() || linked[c.Name] || isInternalPanel(c.Name) {
			continue
		}

		names = append(names, c.Name)
	}

	return names
}

// warnDuplicateIdentities warns about monitors that share an identity. Such
// monitors can't be selected by "edid:" and need "i2c:" instead.
func warnDuplicateIdentities(monitors []Monitor) {
	owners := make(map[string][]int)
	for i, m := range monitors {
		owners[m.EDIDIdentity] = append(owners[m.EDIDIdentity], i)
	}

	for identity, idx := range owners {
		if len(idx) < 2 {
			continue
		}

		slog.Warn("Multiple monitors share an identity, select them with 'i2c:<bus>'.",
			"identity", identity, "buses", busesOf(monitors, idx))
	}
}

// isInternalPanel reports whether a connector is a laptop's built-in panel.
// Panels expose EDID over I2C but never answer DDC/CI.
func isInternalPanel(connectorName string) bool {
	return strings.Contains(connectorName, "-eDP-") || strings.Contains(connectorName, "-LVDS-")
}

// sysfsEDIDAgrees reports whether a connector's sysfs EDID is consistent with
// a monitor's EDID. An empty sysfs EDID is no evidence either way.
func sysfsEDIDAgrees(connector DRMConnector, m Monitor) bool {
	if len(connector.EDID) < 128 {
		return true
	}

	return bytes.Equal(m.EDIDRaw[:], connector.EDID[:128])
}

// resolveConnectorConflicts clears the connector of any monitors that claim
// the same one. Two identical EDIDs can't be told apart, so neither is linked.
func resolveConnectorConflicts(monitors []Monitor) {
	owners := make(map[string][]int)
	for i, m := range monitors {
		if m.DRMConnector != nil {
			owners[*m.DRMConnector] = append(owners[*m.DRMConnector], i)
		}
	}

	for name, idx := range owners {
		if len(idx) < 2 {
			continue
		}

		slog.Warn("Multiple monitors claim the same DRM connector, leaving it unlinked.",
			"connector", name, "buses", busesOf(monitors, idx))

		for _, i := range idx {
			monitors[i].DRMConnector = nil
		}
	}
}

func busesOf(monitors []Monitor, idx []int) []int {
	buses := make([]int, 0, len(idx))
	for _, i := range idx {
		buses = append(buses, monitors[i].I2CBus)
	}

	return buses
}

func readEDIDFromBus(bus int) ([128]byte, error) {
	fd, err := openBus(bus)
	if err != nil {
		return [128]byte{}, fmt.Errorf("opening bus: %w", err)
	}
	defer unix.Close(fd)

	return i2c.ReadEdid(fd)
}

func probeDDCOnBus(bus int) bool {
	fd, err := openBus(bus)
	if err != nil {
		slog.Debug("Failed to open fd for I2C bus.", "error", err, "bus", bus)

		return false
	}
	defer unix.Close(fd)

	return i2c.ProbeDDC(fd)
}
