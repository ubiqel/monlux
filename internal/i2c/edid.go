package i2c

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const edidAddr = uint16(0x50)

var edidHeader = [8]byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

const (
	edidDescriptors    = 54
	edidDescriptorSize = 18
	edidDescriptorNum  = 4
	descriptorName     = byte(0xFC)
	descriptorSerial   = byte(0xFF)
)

// ReadEDID re-reads a bad block before giving up, since bad blocks are usually
// bus glitches.
func (b *Bus) ReadEDID() ([128]byte, error) {
	var edid [128]byte

	err := b.retry("ReadEDID", func() (err error) {
		edid, err = b.readEDIDOnce()

		return err
	})

	return edid, err
}

func (b *Bus) readEDIDOnce() ([128]byte, error) {
	var edid [128]byte
	if err := b.xfer([]message{
		{addr: edidAddr, buf: []byte{0x00}},
		{addr: edidAddr, read: true, buf: edid[:]},
	}); err != nil {
		return [128]byte{}, err
	}

	if !validBlock(edid[:]) {
		return [128]byte{}, fmt.Errorf("%w: EDID header or checksum", errRetryable)
	}

	return edid, nil
}

func validBlock(edid []byte) bool {
	if !bytes.Equal(edid[:8], edidHeader[:]) {
		return false
	}

	var sum byte
	for _, b := range edid[:128] {
		sum += b
	}

	return sum == 0
}

// ParseIdentity builds a "MFG:name:serial" identity from the EDID base block.
//
// Some monitors omit the name or serial descriptors. The name then falls back
// to the product code (hex) and the serial to the numeric serial, or to the
// product code when that is zero. Identical models without descriptors can
// therefore produce the same identity; callers should treat duplicates as
// ambiguous.
func ParseIdentity(edid []byte) (identity string, ok bool) {
	if len(edid) < 128 || !bytes.Equal(edid[:8], edidHeader[:]) {
		return "", false
	}

	mfg := manufacturerID(binary.BigEndian.Uint16(edid[8:10]))
	product := binary.LittleEndian.Uint16(edid[10:12])
	serialNum := binary.LittleEndian.Uint32(edid[12:16])

	var name, serial string

	for base := edidDescriptors; base < edidDescriptors+edidDescriptorNum*edidDescriptorSize; base += edidDescriptorSize {
		d := edid[base : base+edidDescriptorSize]
		if d[0] != 0x00 || d[1] != 0x00 {
			continue
		}

		switch d[3] {
		case descriptorName:
			name = descriptorText(d[5:])
		case descriptorSerial:
			serial = descriptorText(d[5:])
		}
	}

	if name == "" {
		name = fmt.Sprintf("%04X", product)
	}

	if serial == "" {
		if serialNum != 0 {
			serial = strconv.FormatUint(uint64(serialNum), 10)
		} else {
			serial = fmt.Sprintf("%04X", product)
		}
	}

	return strings.Join([]string{mfg, name, serial}, ":"), true
}

// EDID packs three 5-bit letters, where 1 is 'A'.
func manufacturerID(id uint16) string {
	letter := func(shift uint) byte {
		return byte((id>>shift)&0x1F) + 'A' - 1
	}

	return string([]byte{letter(10), letter(5), letter(0)})
}

func descriptorText(raw []byte) string {
	end := len(raw)
	for end > 0 {
		c := raw[end-1]
		if c != ' ' && c != 0x0A && c != 0x00 {
			break
		}

		end--
	}

	return string(raw[:end])
}
