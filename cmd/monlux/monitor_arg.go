package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

func parseMonitorArg(input string) (monitor.Selector, error) {
	if input == "" {
		return monitor.Selector{}, errors.New("missing monitor argument")
	}

	typ, value, hasType := strings.Cut(strings.TrimSpace(input), ":")
	if !hasType {
		return monitor.Selector{Type: monitor.IDTypeDRM, Value: typ}, nil
	}

	switch typ {
	case "drm":
		return monitor.Selector{Type: monitor.IDTypeDRM, Value: value}, nil
	case "edid":
		return monitor.Selector{Type: monitor.IDTypeEDID, Value: value}, nil
	case "i2c":
		return monitor.Selector{Type: monitor.IDTypeI2C, Value: value}, nil
	default:
		return monitor.Selector{}, fmt.Errorf("unknown id type %q, want drm, edid or i2c", typ)
	}
}

func withDisplay(cmd *cli.Command, fn func(bus monitor.Bus, m monitor.Monitor) error) error {
	sel, err := parseMonitorArg(cmd.StringArg(monitorArg))
	if err != nil {
		return fmt.Errorf("parsing monitor argument: %w", err)
	}

	return displayHost(cmd).WithDisplay(sel, fn)
}
