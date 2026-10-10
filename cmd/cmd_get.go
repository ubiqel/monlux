package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/urfave/cli/v3"
)

func runGetCmd(ctx context.Context, cmd *cli.Command) error {
	return withDisplay(cmd.StringArg("DISPLAY"), func(fd int, bus int) error {
		pct, _, _, err := i2c.ReadBrightness(fd)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", bus, err)
		}

		fmt.Printf("%d\n", pct)

		return nil
	})
}
