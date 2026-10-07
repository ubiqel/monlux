package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ubiqel/shine/internal/version"
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
// TODO: add description to <display> argument
func printUsage(output io.Writer) {
	fmt.Fprintf(output,
		`Control external monitor brightness over DDC/CI.

Usage:
  shine [flags]
  shine [command] <args>

Available Commands:
  list                    List detected monitors and their brightness.
  get  <display>          Print brightness of a role as a percentage.
  set  <display> <0-100>  Set brightness of a role.
  up   <display> <step>   Increase brightness by step.
  down <display> <step>   Decrease brightness by step.

Flags:
  -help
    	Show this help.
  -version
    	Show version information.
`)
}
