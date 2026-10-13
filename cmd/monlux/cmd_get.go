package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runGetCmd(_ context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noCacheFlag)

	return withDisplay(cmd, func(fd int, m monitor.Monitor) error {
		pct, _, err := brightness.Current(fd, m, useCache)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", m.I2CBus, err)
		}

		fmt.Printf("%d\n", pct)

		return nil
	})
}
