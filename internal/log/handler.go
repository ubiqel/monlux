package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

type CliHandler struct {
	Out  io.Writer
	Opts slog.HandlerOptions
}

func (h *CliHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.Opts.Level.Level()
}

func (h *CliHandler) Handle(_ context.Context, r slog.Record) error {
	levelStr := fmt.Sprintf("[%s]", r.Level.String())

	var timeStr string
	if h.Opts.Level.Level() <= slog.LevelDebug {
		timeStr = r.Time.Format("15:04:05.000 ")
	}

	var attrs strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		attrs.WriteString(fmt.Sprintf(" %s=%v", a.Key, a.Value))
		return true
	})

	_, err := fmt.Fprintf(h.Out, "%s%s %s%s\n", timeStr, levelStr, r.Message, attrs.String())
	return err
}

func (h *CliHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *CliHandler) WithGroup(name string) slog.Handler       { return h }
