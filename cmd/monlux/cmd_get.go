package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/ubiqel/monlux/internal/i2c"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runGetCmd(_ context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noCacheFlag)

	return withDisplay(cmd, func(fd int, m monitor.Monitor) error {
		pct, raw, maximum, err := i2c.ReadBrightness(fd)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}

		if useCache {
			pct = brightness.Observe(m, pct, raw, maximum)
		} else {
			slog.Debug("Brightness cache disabled, not recording read.", "bus", m.I2CBus)
		}

		fmt.Printf("%d\n", pct)

		return nil
	})
}
