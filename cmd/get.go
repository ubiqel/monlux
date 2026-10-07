package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/urfave/cli/v3"
)

func runGetCmd(ctx context.Context, cmd *cli.Command) error {
	lck, err := lock.Acquire()
	if err != nil {
		return fmt.Errorf("acquiring lock: %w\n", err)
	}
	defer lock.Release(lck)

	displayId, displayIDType, err := parseDisplayID(cmd.StringArg("display"))
	if err != nil {
		return fmt.Errorf("parsing DISPLAY argument: %w", err)
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
		return fmt.Errorf("monitor not found")
	}

	fd, err := openBus(bus)
	if err != nil {
		return fmt.Errorf("opening fd for bus '%v': %w", bus, err)
	}
	pct, _, _, err := i2c.ReadBrightness(fd)
	if err != nil {
		return fmt.Errorf("reading brightness from i2c bus '%v': %w", bus, err)
	}

	fmt.Printf("%d\n", pct)

	return nil
}

const (
	idTypeDRM  = "drm"
	idTypeEDID = "edid"
	idTypeI2C  = "i2c"
)

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
