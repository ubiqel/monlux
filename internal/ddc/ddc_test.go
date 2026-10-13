package ddc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/matryer/is"
	"golang.org/x/sys/unix"
)

func TestParseReplyAcceptsFrames(t *testing.T) {
	tests := []struct {
		name    string
		buf     []byte
		payload []byte
	}{
		{"brightness reply", brightnessReply, []byte{0x02, 0x00, 0x10, 0x01, 0x00, 0x64, 0x00, 0x0F}},
		{"bytes after the frame are ignored", append(frame(0x02, 0x00), 0xAA, 0xBB), []byte{0x02, 0x00}},
		{"empty payload", nullResponse, []byte{}},
		{"length byte without the flag bit", checksummed(0x6E, 0x02, 0x02, 0x00), []byte{0x02, 0x00}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			payload, err := parseReply(tt.buf)
			is.NoErr(err)
			is.Equal(payload, tt.payload)
		})
	}
}

// checksummed appends the monitor-to-host checksum to raw bytes.
func checksummed(data ...byte) []byte {
	return append(data, checksum(seedMonitorToHost, data))
}

func TestParseReplyRejectsBadFrames(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
	}{
		{"source is the host", withSource(frame(0x02, 0x00), 0x51)},
		{"length exceeds the buffer", []byte{0x6E, 0x88, 0x02}},
		{"checksum mismatch", corrupt(nullResponse)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			_, err := parseReply(tt.buf)
			is.True(errors.Is(err, errRetryable))
		})
	}
}

func TestParseReplyRejectsShortBuffers(t *testing.T) {
	for _, buf := range [][]byte{nil, {0x6E}, {0x6E, 0x80}} {
		t.Run(fmt.Sprintf("%d bytes", len(buf)), func(t *testing.T) {
			is := is.New(t)

			_, err := parseReply(buf)
			is.True(err != nil)
			is.True(!errors.Is(err, errRetryable))
		})
	}
}

func TestRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"garbled reply", errRetryable, true},
		{"null response", errNullResponse, true},
		{"NACK", unix.EREMOTEIO, true},
		{"timeout", unix.ETIMEDOUT, true},
		{"wrapped NACK", fmt.Errorf("get VCP 0x10: %w", unix.EREMOTEIO), true},
		{"I/O error", unix.EIO, false},
		{"missing device", unix.ENXIO, false},
		{"bad descriptor", unix.EBADF, false},
		{"busy", unix.EBUSY, false},
		{"wrapped I/O error", fmt.Errorf("get VCP 0x10: %w", unix.EIO), false},
		{"unsupported feature", ErrUnsupportedFeature, false},
		{"other error", errors.New("boom"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(retryable(tt.err), tt.want)
		})
	}
}
