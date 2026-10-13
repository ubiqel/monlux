package ddc

import (
	"errors"
	"fmt"
	"log/slog"
)

// ErrUnsupportedFeature is returned when the monitor says it doesn't implement
// the VCP feature. Retrying can't change that answer.
var ErrUnsupportedFeature = errors.New("VCP feature not supported")

// errNullResponse is retried like a garbled reply. Only when every attempt is
// null does GetBrightness call the feature unsupported, which is how ddcutil
// treats monitors that answer unsupported features with a null message.
var errNullResponse = fmt.Errorf("%w: null response", errRetryable)

const (
	vcpBrightness        = byte(0x10)
	vcpResultUnsupported = byte(0x01)
)

// GetBrightness returns raw units, not percent, because the maximum varies
// between monitors.
func (b *Bus) GetBrightness() (current, maximum uint16, err error) {
	v, err := b.getVCP(vcpBrightness)
	if errors.Is(err, errNullResponse) {
		return 0, 0, fmt.Errorf("%w: null response for 0x%02X", ErrUnsupportedFeature, vcpBrightness)
	}

	if err != nil {
		return 0, 0, err
	}

	if v.maximum == 0 {
		return 0, 0, errors.New("monitor reports a maximum brightness of 0")
	}

	return v.current, v.maximum, nil
}

func (b *Bus) SetBrightness(value uint16) error {
	slog.Debug("Setting new brightness value via i2c", "value", value)

	return b.setVCP(vcpBrightness, value)
}

// ProbeDDC reports whether the monitor answers DDC/CI. A monitor that rejects
// only the brightness feature still answers, so it counts. A null reply doesn't,
// since ddcutil treats that as no answer.
func (b *Bus) ProbeDDC() bool {
	_, err := b.getVCP(vcpBrightness)

	return err == nil || errors.Is(err, ErrUnsupportedFeature)
}

type vcpValue struct {
	current uint16
	maximum uint16
}

func (b *Bus) getVCP(code byte) (vcpValue, error) {
	var v vcpValue

	err := b.retry("GetVCP", func() (err error) {
		v, err = b.getVCPOnce(code)

		return err
	})

	return v, err
}

func (b *Bus) getVCPOnce(code byte) (vcpValue, error) {
	var reply [11]byte
	if err := b.xfer([]message{
		{addr: ddcAddr, buf: request(opGetVCP, code)},
		{addr: ddcAddr, read: true, buf: reply[:]},
	}); err != nil {
		return vcpValue{}, fmt.Errorf("get VCP 0x%02X: %w", code, err)
	}

	payload, err := parseReply(reply[:])
	if err != nil {
		return vcpValue{}, err
	}

	// Some monitors answer a feature they don't know with an empty frame.
	if len(payload) == 0 {
		return vcpValue{}, fmt.Errorf("%w for 0x%02X", errNullResponse, code)
	}

	if len(payload) != 8 || payload[0] != opGetVCPReply || payload[2] != code {
		return vcpValue{}, fmt.Errorf("%w: unexpected reply to get 0x%02X", errRetryable, code)
	}

	if payload[1] == vcpResultUnsupported {
		return vcpValue{}, fmt.Errorf("%w: 0x%02X", ErrUnsupportedFeature, code)
	}

	if payload[1] != 0x00 {
		return vcpValue{}, fmt.Errorf("%w: VCP result 0x%02X for 0x%02X", errRetryable, payload[1], code)
	}

	return vcpValue{
		maximum: uint16(payload[4])<<8 | uint16(payload[5]),
		current: uint16(payload[6])<<8 | uint16(payload[7]),
	}, nil
}

func (b *Bus) setVCP(code byte, value uint16) error {
	return b.retry("SetVCP", func() error {
		return b.setVCPOnce(code, value)
	})
}

func (b *Bus) setVCPOnce(code byte, value uint16) error {
	var reply [6]byte
	if err := b.xfer([]message{
		{addr: ddcAddr, buf: request(opSetVCP, code, byte(value>>8), byte(value))},
		{addr: ddcAddr, read: true, buf: reply[:]},
	}); err != nil {
		return fmt.Errorf("set VCP 0x%02X: %w", code, err)
	}

	// Set replies vary between monitors. Some ACK with an empty frame, which
	// is fine; others echo an opcode, which must be the expected one.
	payload, err := parseReply(reply[:])
	if err != nil {
		return err
	}

	if len(payload) > 0 && payload[0] != setReplyEcho {
		return fmt.Errorf("%w: set reply opcode 0x%02X", errRetryable, payload[0])
	}

	if len(payload) > 1 && payload[1] != 0x00 {
		if payload[1] == vcpResultUnsupported {
			return fmt.Errorf("%w: 0x%02X", ErrUnsupportedFeature, code)
		}

		return fmt.Errorf("%w: set result 0x%02X for 0x%02X", errRetryable, payload[1], code)
	}

	return nil
}
