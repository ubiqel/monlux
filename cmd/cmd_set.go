package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/urfave/cli/v3"
)

func runSetCmd(ctx context.Context, cmd *cli.Command) error {
	lck, err := lock.Acquire()
	if err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer lck.Release()

	return withDisplay(cmd.StringArg("DISPLAY"), func(fd int, bus int) error {
		// Convert percent to real value (it's possible that max is not 100).
		_, _, maximum, err := i2c.ReadBrightness(fd)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", bus, err)
		}
		pct := int(cmd.UintArg("PERCENT"))
		rawVal := uint16(int(maximum) * clamp(pct, 0, 100) / 100)

		if err := i2c.SetBrightness(fd, rawVal); err != nil {
			return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", bus, err)
		}

		return nil
	})
}
