package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/ubiqel/shine/internal/log"
	"github.com/ubiqel/shine/internal/version"
	"github.com/urfave/cli/v3"
)

const (
	verboseFlag = "verbose"
	debugFlag   = "debug"
)

// TODO: add description to <display> argument
// TODO: add up command
// TODO: add down command
// TODO: add set command
// TODO: add cycle command (with --reverse flag)
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	defer handlePanic()

	logHandler := &log.CliHandler{
		Out:  os.Stderr,
		Opts: slog.HandlerOptions{Level: slog.LevelInfo},
	}
	slog.SetDefault(slog.New(logHandler))

	// TODO: configuring debug log level via --verbose flag
	cmd := &cli.Command{
		Name:                   "shine",
		Usage:                  "Control external monitor brightness over DDC/CI.",
		Version:                version.Current().String(),
		UseShortOptionHandling: true,
		HideHelpCommand:        true,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  debugFlag,
				Usage: "Show debug information.",
				Action: func(ctx context.Context, c *cli.Command, debug bool) error {
					if !debug {
						return nil
					}

					logHandler := &log.CliHandler{
						Out:  os.Stderr,
						Opts: slog.HandlerOptions{Level: slog.LevelDebug},
					}
					slog.SetDefault(slog.New(logHandler))

					return nil
				},
			},
		},
		Commands: []*cli.Command{
			{
				Name:            "list",
				Aliases:         []string{"ls"},
				Usage:           "List detected monitors and their brightness.",
				HideHelpCommand: true,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    verboseFlag,
						Aliases: []string{"v"},
						Usage:   "Show extended output.",
					},
				},
				Action: runListCmd,
			}, {
				Name:            "get",
				Usage:           "Print brightness of a monitor as a percentage.",
				HideHelpCommand: true,
				ArgsUsage:       "DISPLAY",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     "display",
						Required: true,
					},
				},
				Action: runGetCmd,
			}, {
				Name:            "set",
				Usage:           "Set brightness of a monitor as a percentage.",
				HideHelpCommand: true,
				ArgsUsage:       "DISPLAY <0-100>",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     "display",
						Required: true,
					},
					&cli.Uint32Arg{
						Name:     "percent",
						Required: true,
					},
				},
				Action: runSetCmd,
			},
		},
	}

	if err := cmd.Run(ctx, os.Args); err != nil {
		slog.Error("Execution failed.", "error", err)
	}
}

func handlePanic() {
	if p := recover(); p != nil {
		slog.Error("Unhandled panic", "panic", p, "stack_trace", debug.Stack())
		os.Exit(1)
	}
}

// TODO: remove later

// func printUsage(output io.Writer) {
// 	fmt.Fprintf(output,
// 		`Control external monitor brightness over DDC/CI.
//
// Usage:
//   shine [flags]
//   shine [command] <args>
//
// Available Commands:
//   list                    List detected monitors and their brightness.
//   get  <display>          Print brightness of a role as a percentage.
//   set  <display> <0-100>  Set brightness of a role.
//   up   <display> <step>   Increase brightness by step.
//   down <display> <step>   Decrease brightness by step.
//
// Flags:
//   -help
//     	Show this help.
//   -version
//     	Show version information.
// `)
// }
