package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/matryer/is"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"runtime error", errors.New("monitor not found"), exitFailure},
		{"usage error", &usageError{msg: "unknown command"}, exitUsage},
		{"wrapped usage error", fmt.Errorf("outer: %w", &usageError{msg: "bad flag"}), exitUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(exitCode(tt.err), tt.want)
		})
	}
}
