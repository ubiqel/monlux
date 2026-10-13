package i2c

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sys/unix"
)

// Frames start with the sender's address byte, because the monitor's address
// 0x37 is already carried by the I2C address.
const (
	ddcAddr       = uint16(0x37)
	hostSource    = byte(0x51)
	monitorSource = byte(0x6E)
	lengthFlag    = byte(0x80)
)

const (
	opGetVCP      = byte(0x01)
	opGetVCPReply = byte(0x02)
	opSetVCP      = byte(0x03)
	setReplyEcho  = byte(0x04)
)

// DDC/CI is occasionally flaky (the monitor sometimes answers the previous
// request), so requests are retried a couple of times with a short pause.
const (
	maxAttempts = 3
	retryPause  = 40 * time.Millisecond
)

var errRetryable = errors.New("unreliable I2C reply")

func request(opcode byte, payload ...byte) []byte {
	frame := append([]byte{hostSource, lengthFlag | byte(1+len(payload)), opcode}, payload...)

	return append(frame, checksum(seedHostToMonitor, frame))
}

// buf can be longer than the frame; the length byte says how much of it is real.
func parseReply(buf []byte) ([]byte, error) {
	if buf[0] != monitorSource {
		return nil, fmt.Errorf("%w: source byte 0x%02X", errRetryable, buf[0])
	}

	n := int(buf[1] & 0x7F)
	if 2+n+1 > len(buf) {
		return nil, fmt.Errorf("%w: length byte 0x%02X", errRetryable, buf[1])
	}

	frame := buf[:2+n+1]
	if checksum(seedMonitorToHost, frame[:len(frame)-1]) != frame[len(frame)-1] {
		return nil, fmt.Errorf("%w: checksum mismatch", errRetryable)
	}

	return frame[2 : 2+n], nil
}

func (b *Bus) retry(name string, op func() error) error {
	var err error
	for attempt := range maxAttempts {
		if err = op(); err == nil || !retryable(err) {
			return err
		}

		slog.Debug(name+" request failed.", "error", err, "attempt", attempt+1)

		if attempt < maxAttempts-1 {
			time.Sleep(b.pause)
		}
	}

	return err
}

// A garbled reply or an I/O error may clear up on retry. An unsupported answer
// or a missing device won't, so those fail at once.
func retryable(err error) bool {
	return errors.Is(err, errRetryable) || errors.Is(err, unix.EIO) || errors.Is(err, unix.EREMOTEIO)
}
