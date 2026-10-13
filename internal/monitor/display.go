package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type IDType int

const (
	IDTypeDRM IDType = iota
	IDTypeEDID
	IDTypeI2C
)

type Selector struct {
	Type IDType
	// For IDTypeI2C, Value is the bus number in decimal.
	Value string
}

func (h Host) WithDisplay(sel Selector, fn func(bus Bus, m Monitor) error) error {
	m, err := h.resolve(sel)
	if err != nil {
		return err
	}

	bus, err := h.OpenBus(m.I2CBus)
	if err != nil {
		return fmt.Errorf("opening fd for bus '%v': %w", m.I2CBus, err)
	}
	defer bus.Close()

	if err := fn(bus, m); err != nil {
		// A sleeping display is the usual reason DDC/CI fails, so say so.
		if state, asleep := h.DPMSState(m); asleep {
			return fmt.Errorf("display is asleep (DPMS %s): %w", state, err)
		}

		return err
	}

	return nil
}

func (h Host) DPMSState(m Monitor) (state string, asleep bool) {
	if m.DRMConnector == nil {
		return "", false
	}

	raw, err := os.ReadFile(filepath.Join(h.DRMPath, *m.DRMConnector, "dpms"))
	if err != nil {
		return "", false
	}

	state = strings.TrimSpace(string(raw))

	// An unknown (empty) state is not treated as asleep.
	return state, state != "" && state != "On"
}

func (h Host) resolve(sel Selector) (Monitor, error) {
	switch sel.Type {
	case IDTypeDRM, IDTypeEDID, IDTypeI2C:
	default:
		return Monitor{}, fmt.Errorf("unknown id type %d", sel.Type)
	}

	monitors := h.Monitors()

	// Collect every match: an identity can be shared by several monitors, and
	// silently picking one would send the command to the wrong display.
	var matches []Monitor
	for _, m := range monitors {
		switch sel.Type {
		case IDTypeDRM:
			if m.DRMConnector == nil || *m.DRMConnector != sel.Value {
				continue
			}
		case IDTypeEDID:
			if m.EDIDIdentity != sel.Value {
				continue
			}
		case IDTypeI2C:
			if strconv.Itoa(m.I2CBus) != sel.Value {
				continue
			}
		}

		matches = append(matches, m)
	}

	switch len(matches) {
	case 0:
		return Monitor{}, fmt.Errorf("no monitor matches %q; run 'monlux list' to see detected monitors", sel.Value)
	case 1:
		return matches[0], nil
	default:
		buses := make([]int, 0, len(matches))
		for _, m := range matches {
			buses = append(buses, m.I2CBus)
		}

		return Monitor{}, fmt.Errorf(
			"%q matches %d monitors (I2C buses %v); select one with i2c:<bus>",
			sel.Value, len(matches), buses,
		)
	}
}
