package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/ubiqel/shine/internal/log"
	"github.com/ubiqel/shine/internal/version"
	"github.com/urfave/cli/v3"
)

const debugFlag = "debug"

func main() {
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
						Name:     "DISPLAY",
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
						Name:     "DISPLAY",
						Required: true,
					},
					&cli.UintArg{
						Name:     "PERCENT",
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
						Name:     "DISPLAY",
						Required: true,
					},
					&cli.UintArg{
						Name:     "STEP",
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
						Name:     "DISPLAY",
						Required: true,
					},
					&cli.UintArg{
						Name:     "STEP",
						Required: true,
					},
				},
				Action: runDownCmd,
			}, {
				Name:            "cycle",
				Usage:           "Cycle predefined steps",
				HideHelpCommand: true,
				ArgsUsage:       "DISPLAY",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     "DISPLAY",
						Required: true,
					},
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "steps",
						Usage: "brightness steps to cycle through",
						Value: "0,50,100",
					},
				},
				Action: runCycleCmd,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s", err)
	}
}
