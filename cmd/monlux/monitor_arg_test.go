package main

import (
	"testing"

	"github.com/matryer/is"
	"github.com/ubiqel/monlux/internal/monitor"
)

func TestParseMonitorArg(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    monitor.Selector
		wantErr bool
	}{
		{"connector", "card1-DP-3", monitor.Selector{Type: monitor.IDTypeDRM, Value: "card1-DP-3"}, false},
		{"explicit drm", "drm:card1-DP-3", monitor.Selector{Type: monitor.IDTypeDRM, Value: "card1-DP-3"}, false},
		{"edid", "edid:GBT1234", monitor.Selector{Type: monitor.IDTypeEDID, Value: "GBT1234"}, false},
		{"i2c bus", "i2c:4", monitor.Selector{Type: monitor.IDTypeI2C, Value: "4"}, false},
		{"surrounding spaces", "  card1-DP-3  ", monitor.Selector{Type: monitor.IDTypeDRM, Value: "card1-DP-3"}, false},
		{"empty", "", monitor.Selector{}, true},
		{"unknown type", "bogus:4", monitor.Selector{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			got, err := parseMonitorArg(tt.input)
			if tt.wantErr {
				is.True(err != nil)

				return
			}

			is.NoErr(err)
			is.Equal(got, tt.want)
		})
	}
}
