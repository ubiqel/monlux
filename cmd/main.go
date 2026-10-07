package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/ubiqel/shine/internal/version"
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
	case "--help", "-h":
		printUsage(os.Stdout)
	case "--version", "-v":
		fmt.Println(version.Current())
	default:
		fmt.Fprintf(os.Stderr, "Unknown command '%s'.\n\n", args[0])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

const commandList = "list"

// TODO: add flags for configuring retries (like in ddcutil)
// TODO: add flag for disabling cache (enabled by default)
// TODO: remove roles and config.ini; use id of display (drm connection or edid identity)
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
	lck, err := lock.Acquire()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to acquire lock: %s", err)
		os.Exit(1)
	}
	defer lock.Release(lck)

	monitors := scanMonitors()

	for _, m := range monitors {
		fd, err := openBus(m.Bus)
		if err != nil {
			// TODO: think on proper logging (warning here)
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
	// TODO: we need to properly get all i2c buses by walking directory instead
	// of this wrongful approach.
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
