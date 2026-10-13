package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"text/tabwriter"

	"github.com/ubiqel/monlux/internal/brightness"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/urfave/cli/v3"
)

type listRow struct {
	bus     int
	drm     string
	edid    string
	reading brightness.Reading
	err     error
}

func runListCmd(_ context.Context, cmd *cli.Command) error {
	host := displayHost(cmd)
	useCache := !cmd.Bool(noCacheFlag)

	rows := fetchListRows(host, host.Monitors(), useCache)

	return renderList(os.Stdout, rows)
}

func fetchListRows(host monitor.Host, monitors []monitor.Monitor, useCache bool) []listRow {
	// The driver may serialize these reads anyway (e.g. Nvidia with 2+ monitors),
	// but parallel reads are never slower.
	var wg sync.WaitGroup
	rows := make([]listRow, len(monitors))
	for i, m := range monitors {
		wg.Go(func() {
			r := listRow{bus: m.I2CBus, edid: m.EDIDIdentity}
			if m.DRMConnector != nil {
				r.drm = *m.DRMConnector
			}
			r.reading, r.err = brightness.Read(host, m, useCache)
			rows[i] = r
		})
	}
	wg.Wait()

	return rows
}

func renderList(w io.Writer, rows []listRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	if _, err := fmt.Fprintln(tw, "I2C\tDRM\tEDID\tBRIGHTNESS"); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(tw, "/dev/i2c-%d\t%s\t%s\t%s\n",
			r.bus, drmText(r), r.edid, brightnessText(r)); err != nil {
			return err
		}
	}

	return tw.Flush()
}

func drmText(r listRow) string {
	if r.drm == "" {
		return "-"
	}

	return r.drm
}

func brightnessText(r listRow) string {
	switch {
	case r.err == nil:
		return fmt.Sprintf("%d%%", r.reading.Percent)
	case r.reading.Asleep:
		return "asleep"
	default:
		return "?"
	}
}
