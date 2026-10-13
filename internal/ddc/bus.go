package ddc

import (
	"fmt"
	"structs"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	i2cMRead     = uint16(0x0001)
	ioctlI2CRdwr = uint32(0x0707)
)

type Bus struct {
	fd int
	// xfer is replaced in tests to script replies without hardware.
	xfer  func(msgs []message) error
	pause time.Duration
}

type message struct {
	addr uint16
	read bool
	buf  []byte
}

// i2cMsg and rdwrData mirror the kernel's struct i2c_msg and
// struct i2c_rdwr_ioctl_data.
type i2cMsg struct {
	_     structs.HostLayout
	Addr  uint16
	Flags uint16
	Len   uint16
	Buf   *byte
}

type rdwrData struct {
	Msgs  *i2cMsg
	NMsgs uint32
}

func Open(bus int) (*Bus, error) {
	fd, err := unix.Open(fmt.Sprintf("/dev/i2c-%d", bus), unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("opening /dev/i2c-%d: %w", bus, err)
	}

	return &Bus{
		fd:    fd,
		xfer:  func(msgs []message) error { return ioctlTransfer(fd, msgs) },
		pause: retryPause,
	}, nil
}

func (b *Bus) Close() error {
	if err := unix.Close(b.fd); err != nil {
		return fmt.Errorf("closing i2c bus: %w", err)
	}

	return nil
}

func ioctlTransfer(fd int, msgs []message) error {
	raw := make([]i2cMsg, len(msgs))
	for i, m := range msgs {
		var flags uint16
		if m.read {
			flags = i2cMRead
		}

		raw[i] = i2cMsg{Addr: m.addr, Flags: flags, Len: uint16(len(m.buf)), Buf: &m.buf[0]}
	}

	data := rdwrData{Msgs: &raw[0], NMsgs: uint32(len(raw))}
	//nolint:gosec // G103: the ioctl needs the raw pointer to the request struct
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(ioctlI2CRdwr), uintptr(unsafe.Pointer(&data)))
	if errno != 0 {
		return fmt.Errorf("i2c transfer: %w", errno)
	}

	return nil
}
