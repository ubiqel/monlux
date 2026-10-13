package ddc

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/matryer/is"
	"golang.org/x/sys/unix"
)

type reply struct {
	data []byte
	err  error
}

type scripted struct {
	t       *testing.T
	replies []reply
	sent    [][]byte
	txns    [][]message
}

func newScriptedBus(t *testing.T, replies ...reply) (*Bus, *scripted) {
	t.Helper()

	s := &scripted{t: t, replies: replies}

	return &Bus{xfer: s.transfer}, s
}

func (s *scripted) transfer(msgs []message) error {
	s.sent = append(s.sent, append([]byte(nil), msgs[0].buf...))

	txn := make([]message, len(msgs))
	for i, m := range msgs {
		txn[i] = message{addr: m.addr, read: m.read, buf: append([]byte(nil), m.buf...)}
	}

	s.txns = append(s.txns, txn)

	if len(s.sent) > len(s.replies) {
		s.t.Fatalf("unscripted transaction %d", len(s.sent))
	}

	r := s.replies[len(s.sent)-1]
	if r.err != nil {
		return r.err
	}

	for _, m := range msgs {
		if m.read {
			copy(m.buf, r.data)
		}
	}

	return nil
}

func frame(payload ...byte) []byte {
	f := append([]byte{0x6E, 0x80 | byte(len(payload))}, payload...)

	return append(f, checksum(seedMonitorToHost, f))
}

func corrupt(data []byte) []byte {
	out := append([]byte(nil), data...)
	out[len(out)-1] ^= 0xFF

	return out
}

var brightnessReply = frame(0x02, 0x00, 0x10, 0x01, 0x00, 0x64, 0x00, 0x0F)

var nullResponse = []byte{0x6E, 0x80, 0xBE}

var brightnessRequest = []byte{0x51, 0x82, 0x01, 0x10, 0xAC}

func TestFrameMatchesVerifiedVectors(t *testing.T) {
	is := is.New(t)

	// Checked against the vectors in checksum_test.go.
	is.Equal(brightnessReply, []byte{0x6E, 0x88, 0x02, 0x00, 0x10, 0x01, 0x00, 0x64, 0x00, 0x0F, 0xCE})
	is.Equal(request(opGetVCP, vcpBrightness), brightnessRequest)
	is.Equal(nullResponse, frame())
}

func TestTransactionsUseDDCAndEDIDAddresses(t *testing.T) {
	is := is.New(t)

	edid := testEDID()
	bus, s := newScriptedBus(t,
		reply{data: brightnessReply}, reply{data: nullResponse}, reply{data: edid[:]})

	_, _, err := bus.GetBrightness()
	is.NoErr(err)
	is.NoErr(bus.SetBrightness(50))
	_, err = bus.ReadEDID()
	is.NoErr(err)

	for _, txn := range s.txns[:2] {
		is.Equal(len(txn), 2)
		is.Equal(txn[0].addr, ddcAddr)
		is.Equal(txn[0].read, false)
		is.Equal(txn[1].addr, ddcAddr)
		is.Equal(txn[1].read, true)
	}

	is.Equal(s.txns[2][0].addr, edidAddr)
	is.Equal(s.txns[2][0].buf, []byte{0x00})
	is.Equal(s.txns[2][1].addr, edidAddr)
	is.Equal(s.txns[2][1].read, true)
}

func TestGetBrightness(t *testing.T) {
	is := is.New(t)

	bus, s := newScriptedBus(t, reply{data: brightnessReply})

	current, maximum, err := bus.GetBrightness()
	is.NoErr(err)
	is.Equal(current, uint16(15))
	is.Equal(maximum, uint16(100))
	is.Equal(s.sent, [][]byte{brightnessRequest})
}

func TestGetBrightnessReadsBothBytes(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		current uint16
		maximum uint16
	}{
		{"high bytes set", frame(0x02, 0x00, 0x10, 0x00, 0x01, 0x90, 0x01, 0x21), 0x0121, 0x0190},
		{"full range", frame(0x02, 0x00, 0x10, 0x00, 0xFF, 0xFF, 0xFF, 0xFE), 0xFFFE, 0xFFFF},
		{"current above maximum is passed through", frame(0x02, 0x00, 0x10, 0x00, 0x00, 0x64, 0x00, 0x78), 120, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, _ := newScriptedBus(t, reply{data: tt.data})

			current, maximum, err := bus.GetBrightness()
			is.NoErr(err)
			is.Equal(current, tt.current)
			is.Equal(maximum, tt.maximum)
		})
	}
}

func TestGetBrightnessRetries(t *testing.T) {
	tests := []struct {
		name    string
		replies []reply
	}{
		{"garbled reply", []reply{{data: corrupt(brightnessReply)}, {data: brightnessReply}}},
		{"NACK", []reply{{err: unix.EREMOTEIO}, {data: brightnessReply}}},
		{"null reply", []reply{{data: nullResponse}, {data: brightnessReply}}},
		{"reply for another feature", []reply{{data: frame(0x02, 0x00, 0x11, 0x00, 0x00, 0x64, 0x00, 0x0F)}, {data: brightnessReply}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, tt.replies...)

			current, _, err := bus.GetBrightness()
			is.NoErr(err)
			is.Equal(current, uint16(15))
			is.Equal(len(s.sent), 2)
		})
	}
}

func TestGetBrightnessGivesUpAfterMaxAttempts(t *testing.T) {
	is := is.New(t)

	bad := corrupt(brightnessReply)
	bus, s := newScriptedBus(t, reply{data: bad}, reply{data: bad}, reply{data: bad})

	_, _, err := bus.GetBrightness()
	is.True(err != nil)
	is.Equal(len(s.sent), maxAttempts)
}

func TestGetBrightnessRetriesIgnoreReplyDetails(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"wrong opcode", frame(0x03, 0x00, 0x10, 0x00, 0x00, 0x64, 0x00, 0x0F)},
		{"payload shorter than 8", frame(0x02, 0x00, 0x10, 0x00, 0x00, 0x64)},
		{"payload longer than 8", frame(0x02, 0x00, 0x10, 0x00, 0x00, 0x64, 0x00, 0x0F, 0x00)},
		{"result 0x02", frame(0x02, 0x02, 0x10, 0x00, 0x00, 0x64, 0x00, 0x0F)},
		{"source is the host", withSource(frame(0x02, 0x00, 0x10, 0x00, 0x00, 0x64, 0x00, 0x0F), 0x51)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, reply{data: tt.data}, reply{data: tt.data}, reply{data: tt.data})

			_, _, err := bus.GetBrightness()
			is.True(err != nil)
			is.True(!errors.Is(err, ErrUnsupportedFeature))
			is.Equal(len(s.sent), maxAttempts)
		})
	}
}

func withSource(data []byte, source byte) []byte {
	out := append([]byte(nil), data...)
	out[0] = source

	return out
}

func TestGetBrightnessDoesNotRetryFatalErrors(t *testing.T) {
	for _, errno := range []unix.Errno{unix.ENXIO, unix.EIO, unix.EBUSY, unix.EBADF} {
		t.Run(errno.Error(), func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, reply{err: errno}, reply{data: brightnessReply})

			_, _, err := bus.GetBrightness()
			is.True(errors.Is(err, errno))
			is.Equal(len(s.sent), 1)
		})
	}
}

func TestGetBrightnessUnsupported(t *testing.T) {
	tests := []struct {
		name    string
		replies []reply
		calls   int
	}{
		{"reported unsupported", []reply{{data: frame(0x02, 0x01, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00)}}, 1},
		{"null response on every attempt", []reply{{data: nullResponse}, {data: nullResponse}, {data: nullResponse}}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, tt.replies...)

			_, _, err := bus.GetBrightness()
			is.True(errors.Is(err, ErrUnsupportedFeature))
			is.Equal(len(s.sent), tt.calls)
		})
	}
}

func TestGetBrightnessRejectsZeroMaximum(t *testing.T) {
	is := is.New(t)

	bus, _ := newScriptedBus(t, reply{data: frame(0x02, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x05)})

	_, _, err := bus.GetBrightness()
	is.True(err != nil)
}

func TestProbeDDC(t *testing.T) {
	garbled := corrupt(brightnessReply)

	tests := []struct {
		name    string
		replies []reply
		want    bool
		calls   int
	}{
		{"answers", []reply{{data: brightnessReply}}, true, 1},
		{"unsupported counts as answering", []reply{{data: frame(0x02, 0x01, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00)}}, true, 1},
		{"recovers from a garbled reply", []reply{{data: garbled}, {data: brightnessReply}}, true, 2},
		{"null reply does not count", []reply{{data: nullResponse}, {data: nullResponse}, {data: nullResponse}}, false, 3},
		{"garbled every time", []reply{{data: garbled}, {data: garbled}, {data: garbled}}, false, 3},
		{"no device", []reply{{err: unix.EREMOTEIO}, {err: unix.EREMOTEIO}, {err: unix.EREMOTEIO}}, false, 3},
		{"missing device", []reply{{err: unix.ENXIO}}, false, 1},
		{"I/O error is not retried", []reply{{err: unix.EIO}}, false, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, tt.replies...)

			is.Equal(bus.ProbeDDC(), tt.want)
			is.Equal(len(s.sent), tt.calls)
		})
	}
}

func TestSetBrightnessRequest(t *testing.T) {
	tests := []struct {
		name  string
		value uint16
		want  []byte
	}{
		{"mid value", 50, []byte{0x51, 0x84, 0x03, 0x10, 0x00, 0x32, 0x9A}},
		{"both bytes set", 0x0123, []byte{0x51, 0x84, 0x03, 0x10, 0x01, 0x23, 0x8A}},
		{"zero", 0, []byte{0x51, 0x84, 0x03, 0x10, 0x00, 0x00, 0xA8}},
		{"maximum", 0xFFFF, []byte{0x51, 0x84, 0x03, 0x10, 0xFF, 0xFF, 0xA8}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, reply{data: nullResponse})

			is.NoErr(bus.SetBrightness(tt.value))
			is.Equal(s.sent, [][]byte{tt.want})
		})
	}
}

func TestSetBrightnessAcceptsReplyShapes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty ACK", nullResponse},
		{"opcode only", frame(setReplyEcho)},
		{"echoed opcode with result 0", frame(setReplyEcho, 0x00)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, reply{data: tt.data})

			is.NoErr(bus.SetBrightness(50))
			is.Equal(len(s.sent), 1)
		})
	}
}

func TestSetBrightnessRejectsBadReply(t *testing.T) {
	failed := frame(setReplyEcho, 0x02)
	wrongOpcode := frame(0x02, 0x00)

	tests := []struct {
		name        string
		replies     []reply
		calls       int
		wantErr     bool
		unsupported bool
	}{
		{"result unsupported", []reply{{data: frame(setReplyEcho, 0x01)}}, 1, true, true},
		{"result failed, then ok", []reply{{data: failed}, {data: nullResponse}}, 2, false, false},
		{"result failed on every attempt", []reply{{data: failed}, {data: failed}, {data: failed}}, 3, true, false},
		{"wrong opcode on every attempt", []reply{{data: wrongOpcode}, {data: wrongOpcode}, {data: wrongOpcode}}, 3, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, tt.replies...)

			err := bus.SetBrightness(50)
			is.Equal(err != nil, tt.wantErr)
			is.Equal(errors.Is(err, ErrUnsupportedFeature), tt.unsupported)
			is.Equal(len(s.sent), tt.calls)
		})
	}
}

func TestSetBrightnessRetriesBadReply(t *testing.T) {
	is := is.New(t)

	bus, s := newScriptedBus(t, reply{data: corrupt(nullResponse)}, reply{data: nullResponse})

	is.NoErr(bus.SetBrightness(50))
	is.Equal(len(s.sent), 2)
}

func TestSetBrightnessFatalErrors(t *testing.T) {
	tests := []struct {
		name    string
		replies []reply
		calls   int
	}{
		{"missing device", []reply{{err: unix.ENXIO}}, 1},
		{"I/O error is not retried", []reply{{err: unix.EIO}}, 1},
		{"NACK on every attempt", []reply{{err: unix.EREMOTEIO}, {err: unix.EREMOTEIO}, {err: unix.EREMOTEIO}}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, tt.replies...)

			is.True(bus.SetBrightness(50) != nil)
			is.Equal(len(s.sent), tt.calls)
		})
	}
}

func testEDID() [128]byte {
	var edid [128]byte
	copy(edid[:], edidHeader[:])
	edid[8], edid[9] = 0x1C, 0x54

	sealEDID(&edid)

	return edid
}

// sealEDID sets the checksum byte so the block sums to zero.
func sealEDID(edid *[128]byte) {
	edid[127] = 0

	var sum byte
	for _, b := range edid[:127] {
		sum += b
	}

	edid[127] = -sum
}

func TestReadEDIDAcceptsValidBlock(t *testing.T) {
	is := is.New(t)

	edid := testEDID()
	bus, s := newScriptedBus(t, reply{data: edid[:]})

	got, err := bus.ReadEDID()
	is.NoErr(err)
	is.Equal(got, edid)
	is.Equal(s.sent, [][]byte{{0x00}})
}

func TestReadEDIDIgnoresExtensionBlocks(t *testing.T) {
	is := is.New(t)

	edid := testEDID()
	edid[126] = 1
	sealEDID(&edid)

	bus, s := newScriptedBus(t, reply{data: edid[:]})

	got, err := bus.ReadEDID()
	is.NoErr(err)
	is.Equal(got, edid)
	is.Equal(len(s.sent), 1)
}

func TestReadEDIDRetriesBadBlock(t *testing.T) {
	is := is.New(t)

	edid := testEDID()
	bad := edid
	bad[100] ^= 0x01

	bus, s := newScriptedBus(t, reply{data: bad[:]}, reply{data: edid[:]})

	got, err := bus.ReadEDID()
	is.NoErr(err)
	is.Equal(got, edid)
	is.Equal(len(s.sent), 2)
}

func TestReadEDIDRejectsBadHeader(t *testing.T) {
	is := is.New(t)

	edid := testEDID()
	edid[0] = 0x01
	sealEDID(&edid)

	bus, s := newScriptedBus(t, reply{data: edid[:]}, reply{data: edid[:]}, reply{data: edid[:]})

	_, err := bus.ReadEDID()
	is.True(err != nil)
	is.Equal(len(s.sent), maxAttempts)
}

func TestReadEDIDRejectsBlockThatStaysBad(t *testing.T) {
	is := is.New(t)

	edid := testEDID()
	edid[100] ^= 0x01

	bus, s := newScriptedBus(t,
		reply{data: edid[:]}, reply{data: edid[:]}, reply{data: edid[:]})

	_, err := bus.ReadEDID()
	is.True(err != nil)
	is.Equal(len(s.sent), maxAttempts)
}

func TestReadEDIDFatalErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		calls int
	}{
		{"missing device", unix.ENXIO, 1},
		{"I/O error is not retried", unix.EIO, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, reply{err: tt.err}, reply{err: tt.err})

			_, err := bus.ReadEDID()
			is.True(errors.Is(err, tt.err))
			is.Equal(len(s.sent), tt.calls)
		})
	}
}

func TestCloseReportsBadDescriptor(t *testing.T) {
	is := is.New(t)

	err := (&Bus{fd: -1}).Close()
	is.True(errors.Is(err, unix.EBADF))
}

func TestCloseReleasesDescriptor(t *testing.T) {
	is := is.New(t)

	fd, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	is.NoErr(err)

	is.NoErr((&Bus{fd: fd}).Close())

	_, err = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	is.True(errors.Is(err, unix.EBADF))
}

// A non-I2C device rejects the request with ENOTTY, so this exercises the
// marshalling and errno wrapping without touching hardware.
func TestIoctlTransferOnNonI2CDevice(t *testing.T) {
	is := is.New(t)

	fd, err := unix.Open("/dev/null", unix.O_RDWR|unix.O_CLOEXEC, 0)
	is.NoErr(err)

	t.Cleanup(func() { _ = unix.Close(fd) })

	err = ioctlTransfer(fd, []message{{addr: ddcAddr, buf: []byte{0x00}}})
	is.True(errors.Is(err, unix.ENOTTY))
}

func TestIoctlTransferRejectsEmptyTransfers(t *testing.T) {
	is := is.New(t)

	is.True(ioctlTransfer(-1, nil) != nil)
	is.True(ioctlTransfer(-1, []message{{addr: ddcAddr}}) != nil)
}

// The kernel reads these structs directly, so a layout mismatch would send
// garbage to the bus without any error.
func TestI2CStructsMatchKernelLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout is checked for 64-bit only")
	}

	is := is.New(t)

	is.Equal(unsafe.Sizeof(i2cMsg{}), uintptr(16))
	is.Equal(unsafe.Offsetof(i2cMsg{}.buf), uintptr(8))
	is.Equal(unsafe.Sizeof(rdwrData{}), uintptr(16))
	is.Equal(unsafe.Offsetof(rdwrData{}.nmsgs), uintptr(8))
}
