package i2c_test

import (
	"testing"

	"github.com/matryer/is"
	"github.com/ubiqel/shine/internal/i2c"
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

	identity, ok := i2c.ParseIdentity(edid[:])
	is.Equal(ok, true)
	is.Equal(identity, "GBT:M27Q P:23133B000026")
}
