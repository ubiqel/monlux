package brightness

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseCycleSteps parses a comma-separated list of percentages, such as
// "0,50,100". It returns the steps and whether they ascend (forward cycle) or
// descend.
func ParseCycleSteps(input string) (steps []uint, forwardCycle bool, err error) {
	parts := strings.Split(input, ",")
	if len(parts) <= 1 {
		return nil, false, errors.New("invalid value format ")
	}

	steps = make([]uint, 0, len(parts))
	for _, p := range parts {
		step, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parsing step \"%v\" as uint: %w", p, err)
		}
		steps = append(steps, uint(step))
	}

	uniqueVals := map[uint]bool{}
	for _, s := range steps {
		if uniqueVals[s] {
			return nil, false, fmt.Errorf("duplicate value '%d'; duplicate values are prohibited", s)
		}
		uniqueVals[s] = true
	}

	// The first two steps set the direction, and every later step must keep
	// going the same way.
	forwardCycle = steps[1] > steps[0]
	for i := 2; i < len(steps); i++ {
		if forwardCycle && steps[i] < steps[i-1] || !forwardCycle && steps[i] > steps[i-1] {
			return nil, false, errors.New("random ordering of steps is prohibited")
		}
	}

	return steps, forwardCycle, nil
}

// NextCycleTarget returns the step after currPct in the cycle. A forward cycle
// moves to the nearest higher step, and a backward cycle to the nearest lower
// one. Past the end of the steps it wraps to the opposite end.
func NextCycleTarget(currPct uint, cycleSteps []uint, forwardCycle bool) uint {
	if forwardCycle {
		for _, step := range cycleSteps {
			if currPct < step {
				return step
			}
		}

		return cycleSteps[0]
	}

	// Compare by value, not position, so the nearest lower step is found even
	// if the steps aren't strictly descending.
	var target uint
	found := false
	highest := cycleSteps[0]
	for _, step := range cycleSteps {
		highest = max(highest, step)

		if currPct > step && (!found || step > target) {
			target = step
			found = true
		}
	}

	if !found {
		return highest
	}

	return target
}
