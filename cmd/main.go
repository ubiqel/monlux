package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/ubiqel/shine/internal/lock"
	"github.com/ubiqel/shine/internal/log"
	"github.com/ubiqel/shine/internal/version"
	"github.com/urfave/cli/v3"
)

const cliName = "shine"

const debugFlag = "debug"

func main() {
	logHandler := &log.CliHandler{
		Out:  os.Stderr,
		Opts: slog.HandlerOptions{Level: slog.LevelInfo},
	}
	slog.SetDefault(slog.New(logHandler))

	var lck *lock.Lock
	stopProfiling := func() {}

	cmd := &cli.Command{
		Name:                   cliName,
		Usage:                  "Control external monitor brightness over DDC/CI.",
		Version:                version.Current().String(),
		UseShortOptionHandling: true,
		HideHelpCommand:        true,
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			l, err := lock.Acquire()
			if err != nil {
				return ctx, fmt.Errorf("acquiring lock: %w", err)
			}
			lck = &l

			if !c.Bool(debugFlag) {
				return ctx, nil
			}

			// Name the profiles after the subcommand, but only if it's a real
			// one, so user input never ends up in a file name.
			name := cliName
			if sub := c.Args().First(); c.Command(sub) != nil {
				name = sub
			}

			stop, err := startProfiling(name)
			if err != nil {
				_ = lck.Release()
				lck = nil

				return ctx, fmt.Errorf("starting profiling: %w", err)
			}
			stopProfiling = stop

			return ctx, nil
		},
		After: func(ctx context.Context, c *cli.Command) error {
			defer stopProfiling()

			if lck == nil {
				return nil
			}

			if err := lck.Release(); err != nil {
				slog.Warn("Failed to release lock.", "error", err)
			}
			lck = nil

			return nil
		},
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
				Action:          runListCmd,
			}, {
				Name:            "detect",
				Usage:           "Explain what was found on each I2C bus and why",
				HideHelpCommand: true,
				Action:          runDetectCmd,
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
				Flags:  []cli.Flag{noBrightnessCacheFlagDef()},
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
				Flags:  []cli.Flag{noBrightnessCacheFlagDef()},
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
				Flags:  []cli.Flag{noBrightnessCacheFlagDef()},
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
				Flags:  []cli.Flag{noBrightnessCacheFlagDef()},
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
					noBrightnessCacheFlagDef(),
				},
				Action: runCycleCmd,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
