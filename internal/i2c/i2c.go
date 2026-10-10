package i2c

import (
	"errors"
	"fmt"
	"log/slog"
	"structs"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type I2CMsg struct {
	_     structs.HostLayout
	Addr  uint16
	Flags uint16
	Len   uint16
	Buf   *byte
}

type I2CRdwrIoctlData struct {
	Msgs  *I2CMsg
	NMsgs uint32
}

const (
	I2C_M_RD = uint16(0x0001)
	I2C_RDWR = uint32(0x0707)
)

const (
	DDC_ADDR  = uint16(0x37)
	EDID_ADDR = uint16(0x50)
)

const VCP_BRIGHTNESS = byte(0x10)

// DDC/CI is occasionally flaky (the monitor sometimes answers the previous
// request), so reads/writes are retried a couple of times with a short delay.
const (
	VCP_RETRIES     = 3
	VCP_RETRY_DELAY = 40 * time.Millisecond
)

func ReadEdid(fd int) ([128]byte, error) {
	var edid [128]byte
	off := [1]byte{0x00}
	msgs := [2]I2CMsg{
		{
			Addr:  EDID_ADDR,
			Flags: 0,
			Len:   1,
			Buf:   &off[0],
		},
		{
			Addr:  EDID_ADDR,
			Flags: I2C_M_RD,
			Len:   128,
			Buf:   &edid[0],
		},
	}

	if err := i2cRdwr(fd, msgs[:]); err != nil {
		return [128]byte{}, fmt.Errorf("i2c_rdwr: %w", err)
	}

	return edid, nil
}

// Brightness as a percentage (0-100), plus the raw values for callers that set.
func ReadBrightness(fd int) (percent int, raw, maximum uint16, err error) { // TODO: i don't like this func. refactor.
	val, e := getVcp(fd, VCP_BRIGHTNESS)
	if e != nil {
		return 0, 0, 0, fmt.Errorf("get_vcp: %w", e)
	}
	if val.maximum == 0 {
		return 0, 0, 0, fmt.Errorf("io error")
	}

	return int(val.current) * 100 / int(val.maximum),
		val.current,
		val.maximum,
		nil
}

func SetBrightness(fd int, value uint16) error {
	slog.Debug("Setting new brightness value via i2c", "value", value)
	return setVcp(fd, VCP_BRIGHTNESS, value)
}

func setVcp(fd int, code byte, value uint16) error {
	var err error
	for attempt := range VCP_RETRIES {
		err = setVcpOnce(fd, code, value)
		if err == nil {
			return nil
		}
		slog.Debug("SetVCP request failed", "error", err, "vcp_code", code)

		// No point sleeping once the final attempt has been made.
		if attempt < VCP_RETRIES-1 {
			slog.Debug("SetVCP sleep before next retry")
			time.Sleep(VCP_RETRY_DELAY)
		}
	}

	return err
}

func setVcpOnce(fd int, code byte, value uint16) error {
	req := [7]byte{0x51, 0x84, 0x03, code, byte(value >> 8), byte(value), 0}
	req[6] = checksumWrite(req[:6])

	var reply [6]byte
	msgs := [2]I2CMsg{
		{Addr: DDC_ADDR, Flags: 0, Len: 7, Buf: &req[0]},
		{Addr: DDC_ADDR, Flags: I2C_M_RD, Len: 6, Buf: &reply[0]},
	}
	if err := i2cRdwr(fd, msgs[:]); err != nil {
		return err
	}

	// Set replies vary between monitors: parse the DDC/CI length byte rather
	// than assuming a fixed frame. Some firmwares ACK with a zero-length
	// message (no opcode), others echo opcode 0x04 and a result.
	data_len := int(reply[1] & 0x7F)
	total := 2 + data_len + 1 // source + length + data + checksum
	if total > len(reply) {
		return errors.New("io error") // TODO: create global var for error
	}
	if checksumRead(reply[:total-1]) != reply[total-1] {
		return errors.New("io error")
	}
	if data_len >= 1 && reply[2] != 0x04 { // Set VCP reply opcode
		return errors.New("io error")
	}

	return nil
}

type VcpValue struct {
	_       structs.HostLayout
	current uint16
	maximum uint16
	kind    uint8
}

// ErrUnsupportedFeature is returned when a monitor explicitly reports that a
// VCP feature is not supported. Retrying cannot change that answer.
var ErrUnsupportedFeature = errors.New("VCP feature not supported")

// errVCPError is returned when a monitor reports a VCP error other than
// "unsupported feature".
var errVCPError = errors.New("VCP error")

func getVcp(fd int, code byte) (val VcpValue, err error) {
	for attempt := range VCP_RETRIES {
		val, err = getVcpOnce(fd, code)
		if err == nil {
			return val, nil
		}

		slog.Debug("GetVCP request failed", "error", err, "vcp_code", code)

		if errors.Is(err, ErrUnsupportedFeature) {
			return VcpValue{}, err
		}

		// No point sleeping once the final attempt has been made.
		if attempt < VCP_RETRIES-1 {
			slog.Debug("GetVCP sleep before next retry")
			time.Sleep(VCP_RETRY_DELAY)
		}
	}

	return VcpValue{}, err
}

func getVcpOnce(fd int, code byte) (val VcpValue, err error) {
	req := [5]byte{0x51, 0x82, 0x01, code, 0}
	req[4] = checksumWrite(req[:4])

	var reply [11]byte
	msgs := [2]I2CMsg{
		{
			Addr:  DDC_ADDR,
			Flags: 0,
			Len:   5,
			Buf:   &req[0],
		},
		{
			Addr:  DDC_ADDR,
			Flags: I2C_M_RD,
			Len:   11,
			Buf:   &reply[0],
		},
	}
	if e := i2cRdwr(fd, msgs[:]); e != nil {
		return VcpValue{}, fmt.Errorf("i2c_rdwr: %w", e)
	}

	if checksumRead(reply[:10]) != reply[10] {
		return VcpValue{}, fmt.Errorf("invalid checksum")
	}
	if reply[2] != 0x02 || reply[4] != code { // get reply, right code
		return VcpValue{}, fmt.Errorf("invalid reply")
	}
	// reply[3] is the DDC/CI result code: 0x00 means no error while 0x01 means
	// the requested VCP feature is unsupported. The latter is a definitive
	// answer, so it must not be retried.
	if reply[3] == 0x01 {
		return VcpValue{}, fmt.Errorf("%w: 0x%02X", ErrUnsupportedFeature, code)
	}
	if reply[3] != 0x00 {
		return VcpValue{}, fmt.Errorf("%w: 0x%02X", errVCPError, reply[3])
	}

	val.kind = reply[5]
	val.maximum = uint16(reply[6])<<8 | uint16(reply[7])
	val.current = uint16(reply[8])<<8 | uint16(reply[9])

	return val, nil
}

func i2cRdwr(fd int, msgs []I2CMsg) error {
	data := I2CRdwrIoctlData{
		Msgs:  &msgs[0],
		NMsgs: uint32(len(msgs)),
	}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(I2C_RDWR), uintptr(unsafe.Pointer(&data)))
	if errno != 0 {
		return fmt.Errorf("running ioctl syscall: %w", errno)
	}

	return nil
}
