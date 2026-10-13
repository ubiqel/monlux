package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/urfave/cli/v3"
)

func runListCmd(_ context.Context, cmd *cli.Command) error {
	host := displayHost(cmd)
	useCache := !cmd.Bool(noCacheFlag)
	monitors := host.Monitors()

	type row struct {
		drm        string
		i2c        string
		brightness string
		edid       string
	}

	// We make parallel requests here to get the brightness, but they may still
	// end up sequential (lock inside driver). For example, it happens on
	// Nvidia card with two monitors connected. On my machine sequential and
	// parallel implementation perform the same so i've kept parallel version.
	var wg sync.WaitGroup
	rows := make([]row, len(monitors))
	for i, m := range monitors {
		wg.Go(func() {
			r := row{
				drm:  "-",
				i2c:  fmt.Sprintf("/dev/i2c-%d", m.I2CBus),
				edid: m.EDIDIdentity,
			}
			if m.DRMConnector != nil {
				r.drm = *m.DRMConnector
			}

			reading, err := brightness.Read(host, m, useCache)
			switch {
			case err == nil:
				r.brightness = fmt.Sprintf("%d%%", reading.Percent)
			case reading.Asleep:
				r.brightness = "asleep"
			default:
				r.brightness = "?"
			}
			rows[i] = r
		})
	}
	wg.Wait()

	// Each ID column holds a value that can be passed as the <monitor> argument.
	headers := []string{"I2C", "DRM", "EDID", "BRIGHTNESS"}
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = len(h)
	}
	for _, r := range rows {
		colWidths[0] = max(colWidths[0], len(r.i2c))
		colWidths[1] = max(colWidths[1], len(r.drm))
		colWidths[2] = max(colWidths[2], len(r.edid))
		colWidths[3] = max(colWidths[3], len(r.brightness))
	}

	fmt.Printf("%-*s  %-*s  %-*s  %s\n",
		colWidths[0], headers[0],
		colWidths[1], headers[1],
		colWidths[2], headers[2],
		headers[3])

	for _, r := range rows {
		fmt.Printf("%-*s  %-*s  %-*s  %s\n",
			colWidths[0], r.i2c,
			colWidths[1], r.drm,
			colWidths[2], r.edid,
			r.brightness)
	}

	return nil
}
