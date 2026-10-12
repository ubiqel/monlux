package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/brightness"
	"github.com/ubiqel/shine/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runSetCmd(_ context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noBrightnessCacheFlag)

	return monitor.DefaultHost().WithDisplay(cmd.StringArg(displayArg), func(fd int, m monitor.Monitor) error {
		// Convert percent to real value (it's possible that max is not 100).
		_, maximum, err := brightness.Current(fd, m, useCache)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}
		pct := int(cmd.UintArg("PERCENT"))
		rawVal := uint16(int(maximum) * brightness.Clamp(pct, 0, 100) / 100)

		if err := brightness.Write(fd, m, rawVal, maximum, useCache); err != nil {
			return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", m.I2CBus, err)
		}

		return nil
	})
}
