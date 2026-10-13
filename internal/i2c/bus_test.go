package i2c

import (
	"errors"
	"testing"

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
}

func newScriptedBus(t *testing.T, replies ...reply) (*Bus, *scripted) {
	t.Helper()

	s := &scripted{t: t, replies: replies}

	return &Bus{xfer: s.transfer}, s
}

func (s *scripted) transfer(msgs []message) error {
	s.sent = append(s.sent, append([]byte(nil), msgs[0].buf...))
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

func TestGetBrightness(t *testing.T) {
	is := is.New(t)

	bus, s := newScriptedBus(t, reply{data: brightnessReply})

	current, maximum, err := bus.GetBrightness()
	is.NoErr(err)
	is.Equal(current, uint16(15))
	is.Equal(maximum, uint16(100))
	is.Equal(s.sent, [][]byte{brightnessRequest})
}

func TestGetBrightnessRetries(t *testing.T) {
	tests := []struct {
		name    string
		replies []reply
	}{
		{"garbled reply", []reply{{data: corrupt(brightnessReply)}, {data: brightnessReply}}},
		{"I/O error", []reply{{err: unix.EIO}, {data: brightnessReply}}},
		{"NACK", []reply{{err: unix.EREMOTEIO}, {data: brightnessReply}}},
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

func TestGetBrightnessDoesNotRetryFatalErrors(t *testing.T) {
	is := is.New(t)

	bus, s := newScriptedBus(t, reply{err: unix.ENXIO}, reply{data: brightnessReply})

	_, _, err := bus.GetBrightness()
	is.True(errors.Is(err, unix.ENXIO))
	is.Equal(len(s.sent), 1)
}

func TestGetBrightnessUnsupported(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"reported unsupported", frame(0x02, 0x01, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00)},
		{"null response", nullResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			bus, s := newScriptedBus(t, reply{data: tt.data})

			_, _, err := bus.GetBrightness()
			is.True(errors.Is(err, ErrUnsupportedFeature))
			is.Equal(len(s.sent), 1)
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
	tests := []struct {
		name    string
		replies []reply
		want    bool
		calls   int
	}{
		{"answers", []reply{{data: brightnessReply}}, true, 1},
		{"null response counts as answering", []reply{{data: nullResponse}}, true, 1},
		{"unsupported counts as answering", []reply{{data: frame(0x02, 0x01, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00)}}, true, 1},
		{"no device", []reply{{err: unix.EREMOTEIO}, {err: unix.EREMOTEIO}, {err: unix.EREMOTEIO}}, false, 3},
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
	is := is.New(t)

	bus, s := newScriptedBus(t, reply{data: nullResponse})

	is.NoErr(bus.SetBrightness(50))
	is.Equal(s.sent, [][]byte{{0x51, 0x84, 0x03, 0x10, 0x00, 0x32, 0x9A}})
}

func TestSetBrightnessAcceptsReplyShapes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty ACK", nullResponse},
		{"echoed opcode", frame(setReplyEcho, 0x00)},
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

func TestSetBrightnessRetriesBadReply(t *testing.T) {
	is := is.New(t)

	bus, s := newScriptedBus(t, reply{data: corrupt(nullResponse)}, reply{data: nullResponse})

	is.NoErr(bus.SetBrightness(50))
	is.Equal(len(s.sent), 2)
}

func testEDID() [128]byte {
	var edid [128]byte
	copy(edid[:], edidHeader[:])
	edid[8], edid[9] = 0x1C, 0x54

	var sum byte
	for _, b := range edid[:127] {
		sum += b
	}

	edid[127] = -sum

	return edid
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
