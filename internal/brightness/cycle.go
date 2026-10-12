package brightness

import (
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

// NextCycleTarget returns the step after currPct in the cycle, wrapping around
// at either end.
func NextCycleTarget(currPct uint, cycleSteps []uint, forwardCycle bool) uint {
	if forwardCycle {
		for _, step := range cycleSteps {
			if currPct < step {
				return step
			}
		}

		return cycleSteps[0]
	}

	for i := len(cycleSteps) - 1; i > 0; i-- {
		if currPct > cycleSteps[i] {
			return cycleSteps[i]
		}
	}

	return cycleSteps[len(cycleSteps)-1]
}
