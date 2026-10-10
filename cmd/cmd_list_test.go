package main

import "testing"

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
			if got := isInternalPanel(tt.connector); got != tt.want {
				t.Errorf("isInternalPanel(%q) = %v, want %v", tt.connector, got, tt.want)
			}
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
			if got := tt.connector.mayBeConnected(); got != tt.want {
				t.Errorf("mayBeConnected() = %v, want %v", got, tt.want)
			}
		})
	}
}
