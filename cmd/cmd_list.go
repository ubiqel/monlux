package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ubiqel/monlux/internal/i2c"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

func runListCmd(_ context.Context, _ *cli.Command) error {
	host := monitor.DefaultHost()
	monitors := host.Monitors()

	type row struct {
		drm        string
		i2c        string
		brightness string
		edid       string
	}

	// TODO: don't like all this shit. refactor
	//
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

			fd, err := i2c.Open(m.I2CBus)
			if err != nil {
				slog.Warn("Failed to open fd for I2C bus", "bus", m.I2CBus)
				r.brightness = "?"
				rows[i] = r

				return
			}

			pct, _, _, err := i2c.ReadBrightness(fd)
			_ = unix.Close(fd)
			switch _, asleep := host.DPMSState(m); {
			case err == nil:
				r.brightness = fmt.Sprintf("%d%%", pct)
			case asleep:
				r.brightness = "asleep"
			default:
				r.brightness = "?"
			}
			rows[i] = r
		})
	}
	wg.Wait()

	// Each ID column holds a value that can be passed as the DISPLAY argument.
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
