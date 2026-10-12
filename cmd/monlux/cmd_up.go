package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runUpCmd(_ context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noBrightnessCacheFlag)

	return monitor.DefaultHost().WithDisplay(cmd.StringArg(monitorArg), func(fd int, m monitor.Monitor) error {
		currPct, maximum, err := brightness.Current(fd, m, useCache)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}

		step := int(cmd.UintArg(stepArg))
		newPct := brightness.Clamp(currPct+step, 0, 100)
		rawVal := uint16(int(maximum) * newPct / 100)
		if err := brightness.Write(fd, m, rawVal, maximum, useCache); err != nil {
			return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", m.I2CBus, err)
		}

		return nil
	})
}
