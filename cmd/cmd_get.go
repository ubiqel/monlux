package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/urfave/cli/v3"
)

func runGetCmd(ctx context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noBrightnessCacheFlag)

	return withDisplay(cmd.StringArg("DISPLAY"), func(fd int, m Monitor) error {
		pct, raw, maximum, err := i2c.ReadBrightness(fd)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}

		if useCache {
			pct = observeBrightness(m, pct, raw, maximum)
		}

		fmt.Printf("%d\n", pct)

		return nil
	})
}
