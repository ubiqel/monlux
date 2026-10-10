package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/urfave/cli/v3"
)

func runGetCmd(ctx context.Context, cmd *cli.Command) error {
	lck, err := lock.Acquire()
	if err != nil {
		return fmt.Errorf("acquiring lock: %w\n", err)
	}
	defer lck.Release()

	return withDisplay(cmd.StringArg("DISPLAY"), func(fd int, bus int) error {
		pct, _, _, err := i2c.ReadBrightness(fd)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", bus, err)
		}

		fmt.Printf("%d\n", pct)

		return nil
	})
}
