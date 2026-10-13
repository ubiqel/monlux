package brightness

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ubiqel/monlux/internal/monitor"
)

type Cycle struct {
	steps   []uint
	forward bool
}

func NewCycle(steps []uint) (Cycle, error) {
	if len(steps) < 2 {
		return Cycle{}, errors.New("a cycle needs at least two steps")
	}

	uniqueVals := map[uint]bool{}
	for _, s := range steps {
		if s > 100 {
			return Cycle{}, fmt.Errorf("step %d is not a percentage from 0 to 100", s)
		}
		if uniqueVals[s] {
			return Cycle{}, fmt.Errorf("step %d is listed more than once", s)
		}
		uniqueVals[s] = true
	}

	forward := steps[1] > steps[0]
	for i := 2; i < len(steps); i++ {
		if forward && steps[i] < steps[i-1] || !forward && steps[i] > steps[i-1] {
			return Cycle{}, errors.New("steps must be strictly ascending or strictly descending")
		}
	}

	return Cycle{steps: slices.Clone(steps), forward: forward}, nil
}

func (c Cycle) Apply(dev Device, m monitor.Monitor, useCache bool) error {
	return adjust(dev, m, useCache, func(raw, maximum uint16) uint16 {
		return percentToRaw(int(c.next(uint(Percent(raw, maximum)))), maximum)
	})
}

func (c Cycle) next(currPct uint) uint {
	if c.forward {
		for _, step := range c.steps {
			if currPct < step {
				return step
			}
		}

		return c.steps[0]
	}

	// Compare by value, not position, so the nearest lower step is found even
	// if the steps aren't strictly descending.
	var target uint
	found := false
	highest := c.steps[0]
	for _, step := range c.steps {
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
