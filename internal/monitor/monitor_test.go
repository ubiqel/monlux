package monitor

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/matryer/is"
)

// absentBus has no /dev/i2c-N node, so opening it fails without touching any
// hardware.
const absentBus = 9999

func TestOpenDDCBusFailureReturnsNilBus(t *testing.T) {
	is := is.New(t)

	// A typed nil *ddc.Bus would be a non-nil Bus, so the nil check below
	// would not catch it.
	bus, err := openDDCBus(absentBus)
	is.True(errors.Is(err, fs.ErrNotExist))
	is.True(bus == nil)
}

func TestProbeDDCOnBusOpenFailure(t *testing.T) {
	is := is.New(t)

	is.True(!probeDDCOnBus(absentBus))
}

func TestReadEDIDFromBusOpenFailure(t *testing.T) {
	is := is.New(t)

	_, err := readEDIDFromBus(absentBus)
	is.True(errors.Is(err, fs.ErrNotExist))
}
