package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/brightness"
	"github.com/ubiqel/shine/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runCycleCmd(ctx context.Context, cmd *cli.Command) error {
	cycleSteps, forwardCycle, err := brightness.ParseCycleSteps(cmd.String("steps"))
	if err != nil {
		return fmt.Errorf("parsing \"--steps\" flag: %w", err)
	}

	useCache := !cmd.Bool(noBrightnessCacheFlag)

	return monitor.DefaultHost().WithDisplay(cmd.StringArg("DISPLAY"), func(fd int, m monitor.Monitor) error {
		currPct, rawMaximum, err := brightness.Current(fd, m, useCache)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}

		targetPct := brightness.NextCycleTarget(uint(currPct), cycleSteps, forwardCycle)
		rawTarget := uint16(uint(rawMaximum) * targetPct / 100)
		if err := brightness.Write(fd, m, rawTarget, rawMaximum, useCache); err != nil {
			return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", m.I2CBus, err)
		}

		return nil
	})
}
