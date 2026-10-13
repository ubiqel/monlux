package ddc_test

import (
	"testing"
	"unicode/utf8"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/ddc"
)

func TestParseIdentity(t *testing.T) {
	is := is.New(t)

	// Minimal EDID: header, mfg "GBT" (0x1C54), product 0x2718, numeric serial 1,
	// descriptor FC = "M27Q P", FF = "23133B000026".
	var edid [128]byte
	copy(edid[:8], []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	edid[8] = 0x1C
	edid[9] = 0x54
	edid[10] = 0x18
	edid[11] = 0x27
	edid[12] = 0x01
	edid[54+3] = 0xFC
	copy(edid[54+5:], []byte{'M', '2', '7', 'Q', ' ', 'P', 0x0A})
	edid[72+3] = 0xFF
	copy(
		edid[72+5:],
		[]byte{'2', '3', '1', '3', '3', 'B', '0', '0', '0', '0', '2', '6', 0x0A},
	)

	identity, ok := ddc.ParseIdentity(edid[:])
	is.Equal(ok, true)
	is.Equal(identity, "GBT:M27Q P:23133B000026")
}

func TestParseIdentityFallbacks(t *testing.T) {
	is := is.New(t)

	header := []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

	// No name or serial descriptors: name falls back to product code (hex),
	// serial to the numeric serial.
	var withSerial [128]byte
	copy(withSerial[:8], header)
	withSerial[8], withSerial[9] = 0x1C, 0x54   // GBT
	withSerial[10], withSerial[11] = 0x18, 0x27 // product 0x2718
	withSerial[12] = 0x01                       // serial 1

	identity, ok := ddc.ParseIdentity(withSerial[:])
	is.Equal(ok, true)
	is.Equal(identity, "GBT:2718:1")

	// No serial at all: serial falls back to the product code.
	var noSerial [128]byte
	copy(noSerial[:8], header)
	noSerial[8], noSerial[9] = 0x1C, 0x54
	noSerial[10], noSerial[11] = 0x18, 0x27

	identity, ok = ddc.ParseIdentity(noSerial[:])
	is.Equal(ok, true)
	is.Equal(identity, "GBT:2718:2718")
}

// edidBase is a block with a valid header, mfg GBT and product 0x2718, and no
// descriptors.
func edidBase() [128]byte {
	var edid [128]byte
	copy(edid[:8], []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00})
	edid[8], edid[9] = 0x1C, 0x54
	edid[10], edid[11] = 0x18, 0x27

	return edid
}

// setText writes a string descriptor with the given tag into slot 0-3.
func setText(edid *[128]byte, slot int, tag byte, text string) {
	d := edid[54+slot*18 : 54+slot*18+18]
	d[3] = tag
	copy(d[5:], text)
}

func identityOf(t *testing.T, edid [128]byte) string {
	t.Helper()

	is := is.New(t)

	identity, ok := ddc.ParseIdentity(edid[:])
	is.True(ok)

	return identity
}

func TestParseIdentityRejectsInvalidBlocks(t *testing.T) {
	base := edidBase()
	short := base[:127]

	badHeader := edidBase()
	badHeader[0] = 0x01

	tests := []struct {
		name string
		edid []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"127 bytes", short},
		{"bad header", badHeader[:]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			_, ok := ddc.ParseIdentity(tt.edid)
			is.True(!ok)
		})
	}
}

func TestParseIdentityFindsDescriptorsInAnySlot(t *testing.T) {
	tests := []struct {
		name string
		set  func(edid *[128]byte)
		want string
	}{
		{"name in the third descriptor, serial in the fourth", func(e *[128]byte) {
			setText(e, 2, 0xFC, "ALPHA\n")
			setText(e, 3, 0xFF, "SERIAL\n")
		}, "GBT:ALPHA:SERIAL"},
		{"name in the fourth descriptor", func(e *[128]byte) {
			setText(e, 3, 0xFC, "BETA\n")
		}, "GBT:BETA:2718"},
		{"last name descriptor wins", func(e *[128]byte) {
			setText(e, 0, 0xFC, "FIRST\n")
			setText(e, 1, 0xFC, "SECOND\n")
		}, "GBT:SECOND:2718"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			edid := edidBase()
			tt.set(&edid)

			is.Equal(identityOf(t, edid), tt.want)
		})
	}
}

// ddcutil only reads string descriptors, which have zero in bytes 0-2 and 4.
// Anything else in that slot is ignored, even when byte 3 looks like a name tag.
func TestParseIdentityIgnoresNonStringDescriptors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(edid *[128]byte)
	}{
		{"timing descriptor", func(e *[128]byte) { e[54] = 0x01 }},
		{"reserved byte 2 set", func(e *[128]byte) { e[54+2] = 0x01 }},
		{"reserved byte 4 set", func(e *[128]byte) { e[54+4] = 0x01 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			edid := edidBase()
			setText(&edid, 0, 0xFC, "FAKE\n")
			tt.mutate(&edid)

			is.Equal(identityOf(t, edid), "GBT:2718:2718")
		})
	}
}

func TestParseIdentityNameText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"blank name falls back to product code", "   \n", "GBT:2718:2718"},
		{"leading spaces kept", "  AB\n", "GBT:  AB:2718"},
		{"trailing spaces trimmed", "AB   ", "GBT:AB:2718"},
		{"text ends at LF", "AB\nCD", "GBT:AB:2718"},
		{"text ends at NUL", "AB\x00CD", "GBT:AB:2718"},
		{"full 13 bytes without a terminator", "ABCDEFGHIJKLM", "GBT:ABCDEFGHIJKLM:2718"},
		{"control byte escaped", "A\x01B\n", "GBT:A<x01>B:2718"},
		{"high byte escaped", "A\xffB\n", "GBT:A<xff>B:2718"},
		{"DEL is kept, as in ddcutil", "A\x7fB\n", "GBT:A\x7fB:2718"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			edid := edidBase()
			setText(&edid, 0, 0xFC, tt.text)

			is.Equal(identityOf(t, edid), tt.want)
		})
	}
}

// The identity is stored as JSON, which replaces invalid UTF-8 and would then
// no longer match the cached value.
func TestParseIdentityIsValidUTF8ForAnyNameByte(t *testing.T) {
	is := is.New(t)

	for c := range 256 {
		edid := edidBase()
		setText(&edid, 0, 0xFC, string([]byte{'A', byte(c), 'B'}))

		is.True(utf8.ValidString(identityOf(t, edid)))
	}
}

func TestParseIdentitySerial(t *testing.T) {
	tests := []struct {
		name string
		set  func(edid *[128]byte)
		want string
	}{
		{"empty serial descriptor falls back to numeric serial", func(e *[128]byte) {
			setText(e, 0, 0xFC, "M27Q\n")
			setText(e, 1, 0xFF, "\n")
			e[12] = 0x01
		}, "GBT:M27Q:1"},
		{"serial descriptor wins over numeric serial", func(e *[128]byte) {
			e[12] = 0x07
			setText(e, 0, 0xFF, "ABC\n")
		}, "GBT:2718:ABC"},
		{"multi-byte numeric serial is little-endian", func(e *[128]byte) {
			e[12], e[13], e[14], e[15] = 0x04, 0x03, 0x02, 0x01
		}, "GBT:2718:16909060"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			edid := edidBase()
			tt.set(&edid)

			is.Equal(identityOf(t, edid), tt.want)
		})
	}
}

func TestParseIdentityProductCodeIsUpperHex(t *testing.T) {
	is := is.New(t)

	edid := edidBase()
	edid[10], edid[11] = 0xCD, 0xAB // product 0xABCD, little-endian

	is.Equal(identityOf(t, edid), "GBT:ABCD:ABCD")
}

// ddcutil doesn't reject manufacturer IDs that aren't letters. It maps each
// 5-bit value to value+64, so zero becomes '@' and 27 becomes '['.
func TestParseIdentityManufacturerID(t *testing.T) {
	tests := []struct {
		name string
		id   [2]byte
		want string
	}{
		{"bit 15 is ignored", [2]byte{0x9C, 0x54}, "GBT"},
		{"first and last letters", [2]byte{0x04, 0x21}, "AAA"},
		{"all Z", [2]byte{0x6B, 0x5A}, "ZZZ"},
		{"zero id", [2]byte{0x00, 0x00}, "@@@"},
		{"value 27 is '['", [2]byte{0x6C, 0x00}, "[@@"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			edid := edidBase()
			edid[8], edid[9] = tt.id[0], tt.id[1]

			is.Equal(identityOf(t, edid), tt.want+":2718:2718")
		})
	}
}
