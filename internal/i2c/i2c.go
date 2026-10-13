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
	i2cMRead     = uint16(0x0001)
	ioctlI2CRdwr = uint32(0x0707)
)

const (
	ddcAddr  = uint16(0x37)
	edidAddr = uint16(0x50)
)

const vcpBrightness = byte(0x10)

// DDC/CI is occasionally flaky (the monitor sometimes answers the previous
// request), so reads/writes are retried a couple of times with a short delay.
const (
	vcpRetries    = 3
	vcpRetryDelay = 40 * time.Millisecond
)

// Open opens /dev/i2c-N for DDC/CI and EDID access. The caller closes the fd.
func Open(bus int) (int, error) {
	fd, err := unix.Open(fmt.Sprintf("/dev/i2c-%d", bus), unix.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("opening /dev/i2c-%d: %w", bus, err)
	}

	return fd, nil
}

func ReadEdid(fd int) ([128]byte, error) {
	var edid [128]byte
	off := [1]byte{0x00}
	msgs := [2]I2CMsg{
		{
			Addr:  edidAddr,
			Flags: 0,
			Len:   1,
			Buf:   &off[0],
		},
		{
			Addr:  edidAddr,
			Flags: i2cMRead,
			Len:   128,
			Buf:   &edid[0],
		},
	}

	if err := i2cRdwr(fd, msgs[:]); err != nil {
		return [128]byte{}, fmt.Errorf("i2c_rdwr: %w", err)
	}

	return edid, nil
}

// ReadBrightness returns the brightness as a percentage (0-100), plus the raw
// values for callers that set.
func ReadBrightness(fd int) (percent int, raw, maximum uint16, err error) {
	val, e := getVcp(fd, vcpBrightness)
	if e != nil {
		return 0, 0, 0, fmt.Errorf("get_vcp: %w", e)
	}
	if val.maximum == 0 {
		return 0, 0, 0, errors.New("io error")
	}

	return int(val.current) * 100 / int(val.maximum),
		val.current,
		val.maximum,
		nil
}

// ProbeDDC reports whether the monitor answers DDC/CI on this bus. A monitor
// that rejects only the brightness feature still answers DDC/CI, so it counts.
func ProbeDDC(fd int) bool {
	_, err := getVcp(fd, vcpBrightness)

	return err == nil || errors.Is(err, ErrUnsupportedFeature)
}

func SetBrightness(fd int, value uint16) error {
	slog.Debug("Setting new brightness value via i2c", "value", value)

	return setVcp(fd, vcpBrightness, value)
}

func setVcp(fd int, code byte, value uint16) error {
	var err error
	for attempt := range vcpRetries {
		err = setVcpOnce(fd, code, value)
		if err == nil {
			return nil
		}
		slog.Debug("SetVCP request failed", "error", err, "vcp_code", code)

		if attempt < vcpRetries-1 {
			slog.Debug("SetVCP sleep before next retry")
			time.Sleep(vcpRetryDelay)
		}
	}

	return err
}

func setVcpOnce(fd int, code byte, value uint16) error {
	req := [7]byte{0x51, 0x84, 0x03, code, byte(value >> 8), byte(value), 0}
	req[6] = checksumWrite(req[:6])

	var reply [6]byte
	msgs := [2]I2CMsg{
		{Addr: ddcAddr, Flags: 0, Len: 7, Buf: &req[0]},
		{Addr: ddcAddr, Flags: i2cMRead, Len: 6, Buf: &reply[0]},
	}
	if err := i2cRdwr(fd, msgs[:]); err != nil {
		return err
	}

	// Set replies vary between monitors: parse the DDC/CI length byte rather
	// than assuming a fixed frame. Some firmwares ACK with a zero-length
	// message (no opcode), others echo opcode 0x04 and a result.
	dataLen := int(reply[1] & 0x7F)
	total := 2 + dataLen + 1
	if total > len(reply) {
		return errors.New("io error")
	}
	if checksumRead(reply[:total-1]) != reply[total-1] {
		return errors.New("io error")
	}
	if dataLen >= 1 && reply[2] != 0x04 {
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

var errVCPError = errors.New("VCP error")

func getVcp(fd int, code byte) (val VcpValue, err error) {
	for attempt := range vcpRetries {
		val, err = getVcpOnce(fd, code)
		if err == nil {
			return val, nil
		}

		slog.Debug("GetVCP request failed", "error", err, "vcp_code", code)

		if errors.Is(err, ErrUnsupportedFeature) {
			return VcpValue{}, err
		}

		if attempt < vcpRetries-1 {
			slog.Debug("GetVCP sleep before next retry")
			time.Sleep(vcpRetryDelay)
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
			Addr:  ddcAddr,
			Flags: 0,
			Len:   5,
			Buf:   &req[0],
		},
		{
			Addr:  ddcAddr,
			Flags: i2cMRead,
			Len:   11,
			Buf:   &reply[0],
		},
	}
	if e := i2cRdwr(fd, msgs[:]); e != nil {
		return VcpValue{}, fmt.Errorf("i2c_rdwr: %w", e)
	}

	if checksumRead(reply[:10]) != reply[10] {
		return VcpValue{}, errors.New("invalid checksum")
	}
	if reply[2] != 0x02 || reply[4] != code {
		return VcpValue{}, errors.New("invalid reply")
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
	//nolint:gosec // G103: the ioctl needs the raw pointer to the request struct
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(ioctlI2CRdwr), uintptr(unsafe.Pointer(&data)))
	if errno != 0 {
		return fmt.Errorf("running ioctl syscall: %w", errno)
	}

	return nil
}
