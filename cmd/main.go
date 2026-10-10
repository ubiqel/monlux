package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ubiqel/shine/internal/log"
	"github.com/ubiqel/shine/internal/version"
	"github.com/urfave/cli/v3"
)

const (
	debugFlag   = "debug"
	verboseFlag = "verbose"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logHandler := &log.CliHandler{
		Out:  os.Stderr,
		Opts: slog.HandlerOptions{Level: slog.LevelInfo},
	}
	slog.SetDefault(slog.New(logHandler))

	cmd := &cli.Command{
		Name:                   "shine",
		Usage:                  "Control external monitor brightness over DDC/CI.",
		Version:                version.Current().String(),
		UseShortOptionHandling: true,
		HideHelpCommand:        true,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  debugFlag,
				Usage: "show debug information during command execution",
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
				Usage:           "List detected monitors and their brightness",
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
				Usage:           "Print brightness of a monitor as a percentage",
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
				Usage:           "Set brightness of a monitor as a percentage",
				HideHelpCommand: true,
				ArgsUsage:       "DISPLAY PERCENT",
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
			}, {
				Name:            "up",
				Usage:           "Increase brightness by step",
				HideHelpCommand: true,
				ArgsUsage:       "DISPLAY STEP",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     "display",
						Required: true,
					},
					&cli.Uint32Arg{
						Name:     "step",
						Required: true,
					},
				},
				Action: runUpCmd,
			}, {
				Name:            "down",
				Usage:           "Decrease brightness by step",
				HideHelpCommand: true,
				ArgsUsage:       "DISPLAY STEP",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     "display",
						Required: true,
					},
					&cli.Uint32Arg{
						Name:     "step",
						Required: true,
					},
				},
				Action: runDownCmd,
			},
		},
	}

	if err := cmd.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s", err)
	}
}
