package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func runUpCmd(ctx context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noBrightnessCacheFlag)

	return withDisplay(cmd.StringArg("DISPLAY"), func(fd int, m Monitor) error {
		currPct, maximum, err := currentBrightness(fd, m, useCache)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}

		step := int(cmd.UintArg("STEP"))
		newPct := clamp(currPct+step, 0, 100)
		rawVal := uint16(int(maximum) * newPct / 100)
		if err := writeBrightness(fd, m, rawVal, maximum, useCache); err != nil {
			return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", m.I2CBus, err)
		}

		return nil
	})
}
