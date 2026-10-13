package main

import (
	"context"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/ubiqel/monlux/internal/i2c"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runSetCmd(_ context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noCacheFlag)

	return withDisplay(cmd, func(bus *i2c.Bus, m monitor.Monitor) error {
		return brightness.SetPercent(bus, m, int(cmd.UintArg(percentArg)), useCache)
	})
}
