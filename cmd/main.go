package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/ubiqel/shine/internal/version"
	"github.com/urfave/cli/v3"
)

const verboseFlag = "verbose"

// TODO: add description to <display> argument
// TODO: add up command
// TODO: add down command
// TODO: add set command
// TODO: add cycle command (with --reverse flag)
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	defer handlePanic()

	logHandler := &CliHandler{
		out:  os.Stderr,
		opts: slog.HandlerOptions{Level: slog.LevelInfo},
	}
	slog.SetDefault(slog.New(logHandler))

	// TODO: configuring debug log level via --verbose flag
	cmd := &cli.Command{
		Name:                   "shine",
		Usage:                  "Control external monitor brightness over DDC/CI.",
		Version:                version.Current().String(),
		UseShortOptionHandling: true,
		HideHelpCommand:        true,
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
						Name:      "display",
						UsageText: "Identificator of display. Can be DRM connection, EDID identificator, I2C bus.",
						Required:  true,
					},
				},
				Action: runGetCmd,
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

type CliHandler struct {
	out  io.Writer
	opts slog.HandlerOptions
}

func (h *CliHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.opts.Level.Level()
}

func (h *CliHandler) Handle(_ context.Context, r slog.Record) error {
	// 1. Format level tag
	levelStr := fmt.Sprintf("[%s]", r.Level.String())

	// 2. Format timestamp only if in DEBUG mode
	var timeStr string
	if h.opts.Level.Level() <= slog.LevelDebug {
		timeStr = r.Time.Format("15:04:05.000 ")
	}

	// 3. Print clean human output: "14:23:01.123 [DEBUG] Message key=value"
	var attrs strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		attrs.WriteString(fmt.Sprintf(" %s=%v", a.Key, a.Value))
		return true
	})

	_, err := fmt.Fprintf(h.out, "%s%s %s%s\n", timeStr, levelStr, r.Message, attrs.String())
	return err
}

func (h *CliHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *CliHandler) WithGroup(name string) slog.Handler       { return h }

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
