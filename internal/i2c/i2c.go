package i2c

import (
	"fmt"
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
func ReadBrightness(fd int) (percent int, raw, maximum uint16, err error) {
	val, e := getVcp(fd, VCP_BRIGHTNESS)
	if e != nil {
		return 0, 0, 0, fmt.Errorf("get_vcp: %w", e)
	}
	if val.maximum == 0 {
		return 0, 0, 0, fmt.Errorf("io error")
		// return 0, 0, 0, .EIO
	}

	return int(val.current) * 100 / int(val.maximum),
		val.current,
		val.maximum,
		nil
}

type VcpValue struct {
	_       structs.HostLayout
	current uint16
	maximum uint16
	kind    uint8
}

func getVcp(fd int, code byte) (val VcpValue, err error) {
	for range VCP_RETRIES {
		val, err = getVcpOnce(fd, code)
		if err == nil {
			return val, nil
		}

		time.Sleep(VCP_RETRY_DELAY)
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
		return VcpValue{}, fmt.Errorf("i2c_rdwr: %w", err)
	}

	if checksumRead(reply[:10]) != reply[10] {
		return VcpValue{}, fmt.Errorf("invalid checksum")
		// return VcpValue{}, .EIO
	}
	if reply[2] != 0x02 || reply[3] != 0x00 || reply[4] != code { // get reply, ok, right code
		return VcpValue{}, fmt.Errorf("invalid something")
		// return VcpValue{}, .EIO
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
