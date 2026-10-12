package monitor

import (
	"testing"

	"github.com/matryer/is"
)

func TestParseDisplayID(t *testing.T) {
	tests := []struct {
		input    string
		wantID   string
		wantType string
		wantErr  bool
	}{
		{"card1-DP-3", "card1-DP-3", idTypeDRM, false},
		{"drm:card1-DP-3", "card1-DP-3", idTypeDRM, false},
		{"edid:GBT1234", "GBT1234", idTypeEDID, false},
		{"i2c:4", "4", idTypeI2C, false},
		{"  card1-DP-3  ", "card1-DP-3", idTypeDRM, false},
		{"", "", "", true},
		{"bogus:4", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			is := is.New(t)

			id, tp, err := parseDisplayID(tt.input)
			if tt.wantErr {
				is.True(err != nil)

				return
			}

			is.NoErr(err)
			is.Equal(id, tt.wantID)
			is.Equal(tp, tt.wantType)
		})
	}
}
