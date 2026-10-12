package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func runSetCmd(ctx context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noBrightnessCacheFlag)

	return withDisplay(cmd.StringArg("DISPLAY"), func(fd int, m Monitor) error {
		// Convert percent to real value (it's possible that max is not 100).
		_, maximum, err := currentBrightness(fd, m, useCache)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}
		pct := int(cmd.UintArg("PERCENT"))
		rawVal := uint16(int(maximum) * clamp(pct, 0, 100) / 100)

		if err := writeBrightness(fd, m, rawVal, maximum, useCache); err != nil {
			return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", m.I2CBus, err)
		}

		return nil
	})
}
