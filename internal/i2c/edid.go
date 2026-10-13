package i2c

import (
	"fmt"
	"strings"
)

// ParseIdentity builds a "MFG:name:serial" identity from the EDID base block.
//
// Some monitors omit the name or serial descriptors. The name then falls back
// to the product code (hex) and the serial to the numeric serial, or to the
// product code when that is zero. Identical models without descriptors can
// therefore produce the same identity; callers should treat duplicates as
// ambiguous.
func ParseIdentity(edid []byte) (identity string, ok bool) {
	if len(edid) < 128 {
		return "", false
	}
	header := [8]byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}
	for i, b := range header {
		if edid[i] != b {
			return "", false
		}
	}

	mfgID := uint16(edid[8])<<8 | uint16(edid[9])
	mfg := [3]byte{
		byte(((mfgID >> 10) & 0x1F) + uint16('A') - 1),
		byte(((mfgID >> 5) & 0x1F) + uint16('A') - 1),
		byte((mfgID & 0x1F) + uint16('A') - 1),
	}

	product := uint32(edid[10]) | uint32(edid[11])<<8
	serialNum :=
		uint32(edid[12]) |
			uint32(edid[13])<<8 |
			uint32(edid[14])<<16 |
			uint32(edid[15])<<24

	var name string
	var serial string
	var nameBuf [16]byte
	var serialBuf [16]byte

	for base := 54; base <= 108; base += 18 {
		if base+18 > len(edid) {
			break
		}
		if edid[base] != 0x00 || edid[base+1] != 0x00 {
			continue
		}
		switch edid[base+3] {
		case 0xFC:
			name = descriptorText(edid[base+5 : base+18])
		case 0xFF:
			serial = descriptorText(edid[base+5 : base+18])
		}
	}

	if name == "" {
		name = string(fmt.Appendf(nameBuf[:0], "%04X", product))
	}
	if serial == "" {
		if serialNum != 0 {
			serial = string(fmt.Appendf(serialBuf[:0], "%d", serialNum))
		} else {
			serial = string(fmt.Appendf(serialBuf[:0], "%04X", product))
		}
	}

	return strings.Join(
			[]string{string(mfg[:]), name, serial},
			":",
		),
		true
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
