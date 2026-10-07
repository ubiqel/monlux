package main

import (
	"errors"
	"fmt"
	"log/slog"
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

func resolveDisplayBus(id string) (int, error) {
	displayId, displayIDType, err := parseDisplayID(id)
	if err != nil {
		return 0, fmt.Errorf("parsing DISPLAY argument: %w", err)
	}

	monitors := scanMonitors()
	var bus int
	var found bool
Loop:
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

		found = true
		bus = m.I2CBus

		break Loop
	}
	if !found {
		return 0, errors.New("monitor not found")
	}

	return bus, nil
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
