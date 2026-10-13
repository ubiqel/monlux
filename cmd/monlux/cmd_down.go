package main

import (
	"context"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runDownCmd(_ context.Context, cmd *cli.Command) error {
	useCache := !cmd.Bool(noCacheFlag)

	return withDisplay(cmd, func(bus monitor.Bus, m monitor.Monitor) error {
		return brightness.Step(bus, m, -int(cmd.UintArg(stepArg)), useCache)
	})
}
