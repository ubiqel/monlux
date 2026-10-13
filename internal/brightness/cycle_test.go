package brightness

import (
	"testing"

	"github.com/matryer/is"
)

func TestNewCycleRejectsInvalidSteps(t *testing.T) {
	tests := []struct {
		name  string
		steps []uint
	}{
		{"single step", []uint{50}},
		{"no steps", nil},
		{"above 100", []uint{0, 50, 150}},
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
