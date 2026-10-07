package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ubiqel/shine/internal/i2c"
	"golang.org/x/sys/unix"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	switch args[0] {
	case commandList:
		runList(args[1:])
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command '%s'.\n\n", args[0])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

const commandList = "list"

func printUsage(output io.Writer) {
	fmt.Fprintf(output,
		`Control external monitor brightness over DDC/CI.

Usage:
  shine [flags]
  shine [command] <args>

Available Commands:
  list                  List detected monitors and their brightness.
  get <role>            Print brightness of a role as a percentage.
  set <role> <0-100>    Set brightness of a role.
  up <role> [step]      Increase brightness by step (default 5).
  down <role> [step]    Decrease brightness by step (default 5).
  cycle <role>          Cycle presets 0 -> 50 -> 100 -> 0.
  cycle-reverse <role>  Cycle presets 100 -> 50 -> 0 -> 100.

Roles are defined in config.ini under a [monitor <role>] section whose
"identity" is the "Monitor:" field from "ddcutil detect --brief".

Flags:
  -h, --help     Show this help.
  -v, --version  Show version information.
`)
}

func runList(args []string) {
	// TODO: lock via flock

	// TODO: scan monitors
	monitors := scanMonitors()
	fmt.Println(monitors)

	// TODO: read brightness for each monitor
	for _, m := range monitors {
		fd, err := openBus(m.Bus)
		if err != nil {
			// TODO: think on proper logging
			fmt.Printf("%s\tbus %d\t?\n", m.Identity, m.Bus)
			continue
		}
		pct, _, _, err := i2c.ReadBrightness(fd)
		unix.Close(fd)
		if err == nil {
			// TODO: save to cache
			fmt.Printf("%s\tbus %d\t%d%%\n", m.Identity, m.Bus, pct)
		} else {
			fmt.Printf("%s\tbus %d\t?\n", m.Identity, m.Bus)
		}

	}
}

type BusInfo struct {
	Bus      int
	Identity string
}

func scanMonitors() []BusInfo {
	var results []BusInfo
	for bus := range 64 {
		fd, err := openBus(bus)
		if err != nil {
			// TODO: log?
			continue
		}

		edid, err := i2c.ReadEdid(fd)
		unix.Close(fd)
		if err != nil {
			// TODO: log?
			continue
		}

		identity, ok := i2c.ParseIdentity(edid[:])
		if !ok {
			// TODO: log?
			continue
		}
		results = append(results, BusInfo{Bus: bus, Identity: identity})
	}

	return results
}

func openBus(bus int) (fd int, err error) {
	return unix.Open(fmt.Sprintf("/dev/i2c-%d", bus), unix.O_RDWR, 0)
}
