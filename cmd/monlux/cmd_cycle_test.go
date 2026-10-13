package main

import (
	"slices"
	"testing"

	"github.com/matryer/is"
)

func TestParseCycleSteps(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantSteps []uint
		wantErr   bool
	}{
		{"list", "0,50,100", []uint{0, 50, 100}, false},
		{"spaces around values", " 0 , 50 ", []uint{0, 50}, false},
		{"single value", "50", []uint{50}, false},
		{"not a number", "0,abc", nil, true},
		{"negative", "0,-5", nil, true},
		{"empty", "", nil, true},
		{"empty value", "0,,50", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			steps, err := parseCycleSteps(tt.input)
			if tt.wantErr {
				is.True(err != nil)

				return
			}

			is.NoErr(err)
			is.True(slices.Equal(steps, tt.wantSteps))
		})
	}
}
