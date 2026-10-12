package i2c

import (
	"testing"

	"github.com/matryer/is"
)

func TestChecksumWrite(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected byte
	}{
		{
			name:     "ddcutil test vector",
			data:     []byte{0x51, 0x82, 0xf5, 0x01},
			expected: 0x49,
		},
		{
			name:     "our verified request",
			data:     []byte{0x51, 0x82, 0x01, 0x10},
			expected: 0xAC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(checksumWrite(tt.data), tt.expected)
		})
	}
}

func TestChecksumRead(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected byte
	}{
		{
			name:     "ddcutil test vector",
			data:     []byte{0x6e, 0x82, 0xa1, 0x00},
			expected: 0x1d,
		},
		{
			name:     "first verified request",
			data:     []byte{0x6e, 0x88, 0x02, 0x00, 0x10, 0x01, 0x00, 0x64, 0x00, 0x0f},
			expected: 0xCE,
		},
		{
			name:     "second verified request",
			data:     []byte{0x6e, 0x88, 0x02, 0x00, 0x10, 0x00, 0x00, 0x64, 0x00, 0x19},
			expected: 0xD9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)
			is.Equal(checksumRead(tt.data), tt.expected)
		})
	}
}
