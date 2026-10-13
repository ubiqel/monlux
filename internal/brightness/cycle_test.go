package brightness

import (
	"testing"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/monitor"
)

func TestNewCycleRejectsInvalidSteps(t *testing.T) {
	tests := []struct {
		name  string
		steps []uint
	}{
		{"single step", []uint{50}},
		{"no steps", nil},
		{"above 100", []uint{0, 50, 150}},
		{"one above 100", []uint{0, 50, 101}},
		{"duplicate", []uint{0, 50, 50}},
		{"unordered after ascending start", []uint{0, 50, 20}},
		{"unordered after descending start", []uint{100, 50, 70}},
		{"trailing step after descending start", []uint{100, 50, 0, 20}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			_, err := NewCycle(tt.steps)
			is.True(err != nil)
		})
	}
}

func TestNewCycleAcceptsValidSteps(t *testing.T) {
	tests := []struct {
		name        string
		steps       []uint
		wantForward bool
	}{
		{"ascending", []uint{0, 50, 100}, true},
		{"descending", []uint{100, 50, 0}, false},
		{"two values", []uint{20, 80}, true},
		{"two values descending", []uint{80, 20}, false},
		{"full range", []uint{0, 100}, true},
		{"descending with uneven gaps", []uint{100, 70, 50, 0}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			c, err := NewCycle(tt.steps)
			is.NoErr(err)
			is.Equal(c.forward, tt.wantForward)
		})
	}
}

func TestCycleNextForward(t *testing.T) {
	c, err := NewCycle([]uint{0, 50, 100})
	is := is.New(t)
	is.NoErr(err)

	tests := []struct {
		name string
		curr uint
		want uint
	}{
		{"below first step", 0, 50},
		{"between steps", 30, 50},
		{"on a step", 50, 100},
		{"at the last step wraps to first", 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(c.next(tt.curr), tt.want)
		})
	}
}

func TestCycleNextBackward(t *testing.T) {
	c, err := NewCycle([]uint{100, 50, 0})
	is := is.New(t)
	is.NoErr(err)

	tests := []struct {
		name string
		curr uint
		want uint
	}{
		{"above the first step", 120, 100},
		{"on the first step", 100, 50},
		{"between steps", 60, 50},
		{"on a middle step", 50, 0},
		{"between the last step and the middle", 10, 0},
		{"at the last step wraps to the first", 0, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(c.next(tt.curr), tt.want)
		})
	}
}

func TestCycleNextBackwardUnsorted(t *testing.T) {
	c, err := NewCycle([]uint{100, 70, 50, 0})
	is := is.New(t)
	is.NoErr(err)

	is.Equal(c.next(60), uint(50))
	is.Equal(c.next(80), uint(70))
	is.Equal(c.next(0), uint(100))
}

func TestCycleNextForwardLastStepBelowHundred(t *testing.T) {
	c, err := NewCycle([]uint{0, 50, 80})
	is := is.New(t)
	is.NoErr(err)

	tests := []struct {
		name string
		curr uint
		want uint
	}{
		{"just below the last step", 79, 80},
		{"on the last step wraps", 80, 0},
		{"above the last step wraps", 90, 0},
		{"at one hundred wraps", 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(c.next(tt.curr), tt.want)
		})
	}
}

func TestCycleNextForwardFirstStepAboveZero(t *testing.T) {
	c, err := NewCycle([]uint{20, 50, 80})
	is := is.New(t)
	is.NoErr(err)

	tests := []struct {
		name string
		curr uint
		want uint
	}{
		{"at zero", 0, 20},
		{"below the first step", 10, 20},
		{"above the last step wraps to the first", 90, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(c.next(tt.curr), tt.want)
		})
	}
}

func TestCycleNextBackwardNoLowerStep(t *testing.T) {
	c, err := NewCycle([]uint{100, 50, 20})
	is := is.New(t)
	is.NoErr(err)

	tests := []struct {
		name string
		curr uint
		want uint
	}{
		{"on the lowest step wraps to the highest", 20, 100},
		{"below the lowest step wraps to the highest", 10, 100},
		{"between the middle and lowest steps", 35, 20},
		{"between the highest and middle steps", 60, 50},
		{"at one hundred goes to the middle step", 100, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(c.next(tt.curr), tt.want)
		})
	}
}

func TestNewCycleCopiesSteps(t *testing.T) {
	is := is.New(t)

	steps := []uint{0, 50, 100}
	c, err := NewCycle(steps)
	is.NoErr(err)

	steps[1] = 10

	is.Equal(c.next(20), uint(50))
}

func TestCycleApplyAdvancesToNextStep(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	c, err := NewCycle([]uint{0, 50, 100})
	is.NoErr(err)

	dev := &fakeDevice{current: 0, maximum: 255}
	is.NoErr(c.Apply(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, false))
	is.Equal(dev.writes, []uint16{128})
}

func TestCycleApplyRoundingCountsAsStep(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	c, err := NewCycle([]uint{0, 50, 100})
	is.NoErr(err)

	dev := &fakeDevice{current: 127, maximum: 255}
	is.NoErr(c.Apply(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, false))
	is.Equal(dev.writes, []uint16{255})
}

func TestCycleApplyUsesCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 0, 255)

	c, err := NewCycle([]uint{0, 50, 100})
	is.NoErr(err)

	// The fresh cache says 0%. The device's 100% would cycle to 0, so this only
	// writes 50% if the cache is trusted.
	dev := &fakeDevice{current: 255, maximum: 255}
	is.NoErr(c.Apply(dev, m, true))
	is.Equal(dev.writes, []uint16{128})
}

func TestCycleApplyReadFailureWritesNothing(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	c, err := NewCycle([]uint{0, 50, 100})
	is.NoErr(err)

	dev := &fakeDevice{current: 0, maximum: 255, getErr: errNoDevice}
	is.True(c.Apply(dev, monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}, false) != nil)
	is.Equal(len(dev.writes), 0)
}

func TestCycleApplyWriteFailureForgetsCache(t *testing.T) {
	isolateCache(t)
	is := is.New(t)

	m := monitor.Monitor{I2CBus: 4, EDIDIdentity: "A"}
	Store(m.I2CBus, m.EDIDIdentity, 0, 255)

	c, err := NewCycle([]uint{0, 50, 100})
	is.NoErr(err)

	is.True(c.Apply(&fakeDevice{maximum: 255, setErr: errNoDevice}, m, true) != nil)

	_, ok := Lookup(m.I2CBus, m.EDIDIdentity)
	is.True(!ok)
}
