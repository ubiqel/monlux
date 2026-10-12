package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/monitor"
	"github.com/urfave/cli/v3"
)

func runDetectCmd(ctx context.Context, cmd *cli.Command) error {
	res := monitor.DefaultHost().Scan()

	if len(res.Buses) == 0 {
		fmt.Println("No candidate I2C buses found.")

		return nil
	}

	for i, r := range res.Buses {
		if i > 0 {
			fmt.Println()
		}

		fmt.Printf("I2C bus %d (/dev/i2c-%d)\n", r.Bus, r.Bus)
		fmt.Printf("   EDID:           %s\n", orDash(r.Identity))
		fmt.Printf("   DRM connector:  %s\n", connectorLabel(r))
		fmt.Printf("   Sysfs EDID:     %s\n", orDash(r.Sysfs))
		fmt.Printf("   Internal panel: %s\n", yesNo(r.Panel))
		fmt.Printf("   DDC/CI:         %s\n", ddcLabel(r))
		fmt.Printf("   Result:         %s\n", r.Result)
	}

	if len(res.Unlinked) > 0 {
		fmt.Println()
		fmt.Println("Connectors with no monitor linked (status may be unreliable):")
		for _, name := range res.Unlinked {
			fmt.Printf("   %s\n", name)
		}
	}

	return nil
}

func connectorLabel(r monitor.BusReport) string {
	if r.Connector == "" {
		return "-"
	}

	if r.Link == "" {
		return r.Connector
	}

	return fmt.Sprintf("%s (linked by %s)", r.Connector, r.Link)
}

func ddcLabel(r monitor.BusReport) string {
	switch {
	case !r.Probed:
		return "not probed"
	case r.DDC:
		return "yes"
	default:
		return "no"
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}
