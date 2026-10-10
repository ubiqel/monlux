package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/urfave/cli/v3"
)

func runCycleCmd(ctx context.Context, cmd *cli.Command) error {
	lck, err := lock.Acquire()
	if err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer lck.Release()

	cycleSteps, forwardCycle, err := parseCycleSteps(cmd)
	if err != nil {
		return fmt.Errorf("parsing \"--steps\" flag: %w", err)
	}

	return withDisplay(cmd.StringArg("DISPLAY"), func(fd int, bus int) error {
		currPct, _, rawMaximum, err := i2c.ReadBrightness(fd)
		if err != nil {
			return fmt.Errorf("reading brightness from i2c bus '%v': %w", bus, err)
		}

		targetPct := findNextCycleTarget(uint(currPct), cycleSteps, forwardCycle)
		rawTarget := uint16(uint(rawMaximum) * targetPct / 100)
		if err := i2c.SetBrightness(fd, rawTarget); err != nil {
			return fmt.Errorf("setting new brightness value via i2c bus '%v': %w", bus, err)
		}

		return nil
	})
}

func parseCycleSteps(cmd *cli.Command) (steps []uint, forwardCycle bool, err error) {
	parts := strings.Split(cmd.String("steps"), ",")
	if len(parts) <= 1 {
		return nil, false, fmt.Errorf("invalid value format ")
	}

	steps = make([]uint, 0, len(parts))
	for _, p := range parts {
		step, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parsing step \"%v\" as uint: %w", p, err)
		}
		steps = append(steps, uint(step))
	}

	// search for duplicates and also find ordering of elements.
	uniqueVals := map[uint]bool{}
	forwardCycle = true
	for i, s := range steps {
		if uniqueVals[s] {
			return nil, false, fmt.Errorf("duplicate value '%d'; duplicate values are prohibited", s)
		}
		uniqueVals[s] = true

		if i == 0 {
			continue
		}
		if i == 1 {
			if s < steps[i-1] {
				forwardCycle = false
			}

			continue
		}

		if s < steps[i-1] && forwardCycle {
			return nil, false, fmt.Errorf("random ordering of steps is prohibited")
		}
	}

	return steps, forwardCycle, nil
}

func findNextCycleTarget(currtPct uint, cycleSteps []uint, forwardCycle bool) uint {
	if forwardCycle {
		for _, step := range cycleSteps {
			if currtPct < step {
				return step
			}
		}

		return cycleSteps[0]
	}

	for i := len(cycleSteps) - 1; i > 0; i-- {
		if currtPct > cycleSteps[i] {
			return cycleSteps[i]
		}
	}

	return cycleSteps[len(cycleSteps)-1]
}
