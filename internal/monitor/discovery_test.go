package monitor

import (
	"testing"

	"github.com/matryer/is"
)

func TestBusFromPath(t *testing.T) {
	tests := []struct {
		path string
		want int
		ok   bool
	}{
		{"/dev/i2c-4", 4, true},
		{"/sys/bus/i2c/devices/i2c-12", 12, true},
		{"i2c-0", 0, true},
		{"/dev/i2c-", 0, false},
		{"/dev/i2c-x", 0, false},
		{"/dev/card0", 0, false},
		{"/dev/xi2c-4", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			is := is.New(t)

			got, ok := busFromPath(tt.path)
			is.Equal(ok, tt.ok)
			is.Equal(got, tt.want)
		})
	}
}
