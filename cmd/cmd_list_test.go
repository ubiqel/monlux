package main

import (
	"testing"

	"github.com/matryer/is"
)

func edidWith(seed byte) [128]byte {
	var e [128]byte
	for i := range e {
		e[i] = seed + byte(i)
	}

	return e
}

func TestSysfsEDIDAgrees(t *testing.T) {
	monitorEDID := edidWith(1)
	m := Monitor{I2CBus: 4, EDIDRaw: monitorEDID}

	same := make([]byte, 128)
	copy(same, monitorEDID[:])

	other := edidWith(2)

	tests := []struct {
		name      string
		connector DRMConnector
		want      bool
	}{
		{"same EDID", DRMConnector{EDID: same}, true},
		{"same EDID with extension block", DRMConnector{EDID: append(same, 0xAA, 0xBB)}, true},
		{"different EDID (stale link)", DRMConnector{EDID: other[:]}, false},
		{"no sysfs EDID", DRMConnector{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(sysfsEDIDAgrees(tt.connector, m), tt.want)
		})
	}
}

func TestResolveConnectorConflicts(t *testing.T) {
	is := is.New(t)

	name := "card1-DP-3"
	other := "card1-DP-2"

	monitors := []Monitor{
		{I2CBus: 4, DRMConnector: &name},
		{I2CBus: 5, DRMConnector: &name},
		{I2CBus: 6, DRMConnector: &other},
	}

	resolveConnectorConflicts(monitors)

	is.True(monitors[0].DRMConnector == nil)
	is.True(monitors[1].DRMConnector == nil)
	is.True(monitors[2].DRMConnector != nil && *monitors[2].DRMConnector == other)
}

func TestIsInternalPanel(t *testing.T) {
	tests := []struct {
		connector string
		want      bool
	}{
		{"card1-eDP-1", true},
		{"card0-LVDS-1", true},
		{"card1-DP-3", false},
		{"card0-HDMI-A-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.connector, func(t *testing.T) {
			is := is.New(t)

			is.Equal(isInternalPanel(tt.connector), tt.want)
		})
	}
}

func TestMayBeConnected(t *testing.T) {
	tests := []struct {
		name      string
		connector DRMConnector
		want      bool
	}{
		{"connected, reliable status", DRMConnector{Connected: true, StatusReliable: true}, true},
		{"disconnected, reliable status", DRMConnector{Connected: false, StatusReliable: true}, false},
		{"disconnected, unreliable status", DRMConnector{Connected: false, StatusReliable: false}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := is.New(t)

			is.Equal(tt.connector.mayBeConnected(), tt.want)
		})
	}
}
