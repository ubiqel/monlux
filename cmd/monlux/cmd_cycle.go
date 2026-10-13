package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runCycleCmd(_ context.Context, cmd *cli.Command) error {
	steps, err := parseCycleSteps(cmd.String("steps"))
	if err != nil {
		return fmt.Errorf("invalid --steps: %w", err)
	}

	cycle, err := brightness.NewCycle(steps)
	if err != nil {
		return fmt.Errorf("invalid --steps: %w", err)
	}

	useCache := !cmd.Bool(noCacheFlag)

	return withDisplay(cmd, func(bus monitor.Bus, m monitor.Monitor) error {
		return cycle.Apply(bus, m, useCache)
	})
}

func parseCycleSteps(input string) ([]uint, error) {
	parts := strings.Split(input, ",")
	steps := make([]uint, 0, len(parts))
	for _, p := range parts {
		step, err := strconv.ParseUint(strings.TrimSpace(p), 10, 8)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number from 0 to 100", p)
		}
		steps = append(steps, uint(step))
	}

	return steps, nil
}
