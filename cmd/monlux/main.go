package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/ubiqel/monlux/internal/lock"
	"github.com/ubiqel/monlux/internal/log"
	"github.com/ubiqel/monlux/internal/monitor"
	"github.com/ubiqel/monlux/internal/profile"
	"github.com/ubiqel/monlux/internal/version"
	"github.com/urfave/cli/v3"
)

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
		Usage:                  "control external monitor brightness over DDC/CI",
		Description:            rootDescription,
		Version:                version.Current().String(),
		UseShortOptionHandling: true,
		OnUsageError:           onUsageError,
		Action:                 runRootCmd,
		// Errors are printed and turned into exit codes by main. Letting the
		// library exit on its own would skip the lock release in After.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
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
			commandName := cliName
			if sub := c.Args().First(); c.Command(sub) != nil {
				commandName = sub
			}

			stop, err := profile.StartProfiling(cliName, commandName)
			if err != nil {
				_ = lck.Release()
				lck = nil

				return ctx, fmt.Errorf("starting profiling: %w", err)
			}
			stopProfiling = stop

			return ctx, nil
		},
		After: func(_ context.Context, _ *cli.Command) error {
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
				Name:    debugFlag,
				Aliases: []string{"d"},
				Usage:   "print debug logs and write CPU profile and trace files to the cache directory",
				Action: func(_ context.Context, _ *cli.Command, debug bool) error {
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
			&cli.BoolFlag{
				Name:  noCacheFlag,
				Usage: "scan monitors and read and write brightness on the monitor, ignoring the caches",
			},
		},
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list detected monitors and their brightness",
				Description: `Print one row per detected monitor, with its I2C bus, DRM connector, EDID
identity and current brightness.

Use the DRM connector as <monitor>. The EDID identity needs the edid: prefix,
and the bus number needs the i2c: prefix.

Brightness is "asleep" while the display is in DPMS sleep, and "?" when it
could not be read.

Examples:
  monlux list`,
				Action: runListCmd,
			}, {
				Name:  "detect",
				Usage: "explain how each I2C bus was classified",
				Description: `For each I2C bus, show the EDID found on it, the DRM connector it was linked
to, whether it answers DDC/CI, and the final result. Use it to find out why a
monitor is missing from "monlux list".

Examples:
  monlux detect`,
				Action: runDetectCmd,
			}, {
				Name:      "get",
				Usage:     "print the brightness of a monitor as a percentage",
				ArgsUsage: "<monitor>",
				Description: `Print the brightness of <monitor> as a number from 0 to 100. There is no
percent sign, so the output is easy to use in scripts.

` + monitorDescription + `

Examples:
  monlux get card1-DP-2`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     monitorArg,
						Required: true,
					},
				},
				Action: runGetCmd,
			}, {
				Name:      "set",
				Usage:     "set the brightness of a monitor to a percentage",
				ArgsUsage: "<monitor> <percent>",
				Description: `Set the brightness of <monitor> to <percent>, from 0 to 100. Values above 100
are set to 100.

` + monitorDescription + `

Examples:
  monlux set card1-DP-2 60`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     monitorArg,
						Required: true,
					},
					&cli.UintArg{
						Name:     percentArg,
						Required: true,
					},
				},
				Action: runSetCmd,
			}, {
				Name:      "up",
				Usage:     "raise the brightness of a monitor by a step",
				ArgsUsage: "<monitor> <step>",
				Description: `Raise the brightness of <monitor> by <step> percentage points. Brightness
stops at 100.

` + monitorDescription + `

Examples:
  monlux up card1-DP-2 10`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     monitorArg,
						Required: true,
					},
					&cli.UintArg{
						Name:     stepArg,
						Required: true,
					},
				},
				Action: runUpCmd,
			}, {
				Name:      "down",
				Usage:     "lower the brightness of a monitor by a step",
				ArgsUsage: "<monitor> <step>",
				Description: `Lower the brightness of <monitor> by <step> percentage points. Brightness
stops at 0.

` + monitorDescription + `

Examples:
  monlux down card1-DP-2 10`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     monitorArg,
						Required: true,
					},
					&cli.UintArg{
						Name:     stepArg,
						Required: true,
					},
				},
				Action: runDownCmd,
			}, {
				Name:      "cycle",
				Usage:     "cycle the brightness of a monitor through predefined steps",
				ArgsUsage: "<monitor>",
				Description: `Set the brightness of <monitor> to the next value in --steps.

The first two steps set the direction. If the second is higher, the cycle goes
up; if it is lower, the cycle goes down. Every later step must keep that
direction.

Going up, the next value is the first step above the current brightness. Going
down, it is the first step below it. Past the last step, the cycle wraps around
to the other end.

` + monitorDescription + `

Examples:
  monlux cycle card1-DP-2
  monlux cycle card1-DP-2 --steps 20,40,60,80`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:     monitorArg,
						Required: true,
					},
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "steps",
						Usage: "comma-separated percentages to cycle through, strictly ascending or descending",
						Value: "0,50,100",
					},
				},
				Action: runCycleCmd,
			},
		},
	}

	for _, sub := range cmd.Commands {
		sub.OnUsageError = onUsageError
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(exitCode(err))
	}
}

const cliName = "monlux"

const monitorArg = "monitor"

const (
	percentArg = "percent"
	stepArg    = "step"
)

const (
	debugFlag   = "debug"
	noCacheFlag = "no-cache"
)

const (
	exitFailure = 1
	exitUsage   = 2
)

const rootDescription = `monlux controls the brightness of external monitors over DDC/CI. It finds
monitors through /sys/class/drm and /dev/i2c-*, so it needs no other tools.

Brightness is always a percentage from 0 to 100, whatever scale the monitor
uses internally.

Examples:
  monlux list
  monlux set card1-DP-2 60
  monlux up card1-DP-2 10

Exit status:
  0  success
  1  failure, such as an unknown monitor or a failed I2C transfer
  2  invalid usage, such as an unknown command, flag or argument`

const monitorDescription = `<monitor> names one monitor. Give its DRM connector, such as card1-DP-2, as
printed by "monlux list". Other forms: drm:<connector>, edid:<identity> and
i2c:<bus>. If several monitors match, use i2c:<bus>.`

func displayHost(cmd *cli.Command) monitor.Host {
	host := monitor.DefaultHost()
	host.NoCache = cmd.Bool(noCacheFlag)

	return host
}

type usageError struct {
	msg string
}

func (e *usageError) Error() string { return e.msg }

func (e *usageError) ExitCode() int { return exitUsage }

func newUsageError(cmd *cli.Command, msg string) error {
	return &usageError{msg: fmt.Sprintf("%s\nRun '%s --help' for usage.", msg, cmd.FullName())}
}

func onUsageError(_ context.Context, cmd *cli.Command, err error, _ bool) error {
	return newUsageError(cmd, err.Error())
}

func runRootCmd(_ context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() == 0 {
		return newUsageError(cmd, "missing command")
	}

	return newUsageError(cmd, fmt.Sprintf("unknown command %q", cmd.Args().First()))
}

func exitCode(err error) int {
	if coder, ok := errors.AsType[cli.ExitCoder](err); ok {
		return coder.ExitCode()
	}

	return exitFailure
}
