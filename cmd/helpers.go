package main

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	idTypeDRM  = "drm"
	idTypeEDID = "edid"
	idTypeI2C  = "i2c"
)

// withDisplay resolves a DISPLAY argument, opens its I2C bus and runs fn with
// the fd. The fd is always closed. If fn fails while the display is asleep, the
// error says so, since a sleeping display is the usual reason DDC/CI fails.
func withDisplay(id string, fn func(fd int, bus int) error) error {
	m, err := resolveDisplay(id)
	if err != nil {
		return err
	}

	fd, err := openBus(m.I2CBus)
	if err != nil {
		return fmt.Errorf("opening fd for bus '%v': %w", m.I2CBus, err)
	}
	defer unix.Close(fd)

	if err := fn(fd, m.I2CBus); err != nil {
		if state, asleep := dpmsState(m); asleep {
			return fmt.Errorf("display is asleep (DPMS %s): %w", state, err)
		}

		return err
	}

	return nil
}

// dpmsState returns the DPMS state of the monitor's DRM connector (e.g. "On",
// "Standby", "Off") and whether it's asleep. Unknown states are not asleep.
func dpmsState(m Monitor) (state string, asleep bool) {
	if m.DRMConnector == nil {
		return "", false
	}

	raw, err := os.ReadFile(filepath.Join(drmPath, *m.DRMConnector, "dpms"))
	if err != nil {
		return "", false
	}

	state = strings.TrimSpace(string(raw))

	return state, state != "" && state != "On"
}

func resolveDisplay(id string) (Monitor, error) {
	displayId, displayIDType, err := parseDisplayID(id)
	if err != nil {
		return Monitor{}, fmt.Errorf("parsing DISPLAY argument: %w", err)
	}

	monitors := scanMonitors()

	// Collect every match: an identity can be shared by several monitors, and
	// silently picking one would send the command to the wrong display.
	var matches []Monitor
	for _, m := range monitors {
		switch displayIDType {
		case idTypeDRM:
			if m.DRMConnector == nil || *m.DRMConnector != displayId {
				continue
			}
		case idTypeEDID:
			if m.EDIDIdentity != displayId {
				continue
			}
		case idTypeI2C:
			// in this branch we just check that bus exists.
			if strconv.Itoa(m.I2CBus) != displayId {
				continue
			}
		default:
			// should be unrecheable.
			slog.Debug("Reached unrecheable branch in switch statement.")

			continue
		}

		matches = append(matches, m)
	}

	switch len(matches) {
	case 0:
		return Monitor{}, errors.New("monitor not found")
	case 1:
		return matches[0], nil
	default:
		buses := make([]int, 0, len(matches))
		for _, m := range matches {
			buses = append(buses, m.I2CBus)
		}

		return Monitor{}, fmt.Errorf(
			"display '%s' matches %d monitors (buses %v); select one with 'i2c:<bus>'",
			displayId, len(matches), buses,
		)
	}
}

func parseDisplayID(input string) (id string, tp string, err error) {
	if input == "" {
		return "", "", errors.New("missing display argument")
	}

	parts := strings.SplitN(strings.TrimSpace(input), ":", 2)
	if len(parts) == 1 {
		return parts[0], idTypeDRM, nil
	}

	tp = parts[0]
	id = parts[1]
	switch tp {
	case idTypeDRM, idTypeEDID, idTypeI2C:
		return id, tp, nil
	default:
		return "", "", fmt.Errorf("unknown display id type '%s'", tp)
	}
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

func clamp[T cmp.Ordered](value, minimum, maximum T) T {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
