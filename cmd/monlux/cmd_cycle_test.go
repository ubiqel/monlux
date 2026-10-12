package main

import (
	"slices"
	"testing"

	"github.com/matryer/is"
)

func TestParseCycleSteps(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSteps   []uint
		wantForward bool
		wantErr     bool
	}{
		{"ascending", "0,50,100", []uint{0, 50, 100}, true, false},
		{"descending", "100,50,0", []uint{100, 50, 0}, false, false},
		{"two values", "20,80", []uint{20, 80}, true, false},
		{"single value", "50", nil, false, true},
		{"not a number", "0,abc", nil, false, true},
		{"negative", "0,-5", nil, false, true},
		{"above 100", "0,50,150", nil, false, true},
		{"duplicate", "0,50,50", nil, false, true},
		{"unordered after ascending start", "0,50,20", nil, false, true},
		{"unordered after descending start", "100,50,70", nil, false, true},
		{"trailing step after descending start", "100,50,0,20", nil, false, true},
		{"descending with uneven gaps", "100,70,50,0", []uint{100, 70, 50, 0}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			steps, forward, err := parseCycleSteps(tt.input)
			if tt.wantErr {
				is.True(err != nil)

				return
			}

			is.NoErr(err)
			is.True(slices.Equal(steps, tt.wantSteps))
			is.Equal(forward, tt.wantForward)
		})
	}
}

func TestNextCycleTargetForward(t *testing.T) {
	steps := []uint{0, 50, 100}

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
			is.Equal(nextCycleTarget(tt.curr, steps, true), tt.want)
		})
	}
}

func TestNextCycleTargetBackward(t *testing.T) {
	steps := []uint{100, 50, 0}

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
			is.Equal(nextCycleTarget(tt.curr, steps, false), tt.want)
		})
	}
}

func TestNextCycleTargetBackwardUnsorted(t *testing.T) {
	// Steps need not be evenly spaced, so the nearest lower step is found by
	// value.
	steps := []uint{100, 70, 50, 0}

	is := is.New(t)

	is.Equal(nextCycleTarget(60, steps, false), uint(50))
	is.Equal(nextCycleTarget(80, steps, false), uint(70))
	is.Equal(nextCycleTarget(0, steps, false), uint(100))
}
