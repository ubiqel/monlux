package profile

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/pprof"
	"runtime/trace"
	"time"
)

func StartProfiling(cliName, commandName string) (stop func(), err error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("finding cache directory: %w", err)
	}

	dir := filepath.Join(cacheDir, cliName, "debug")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}

	stamp := time.Now().Format("20060102-150405")
	cpuPath := filepath.Join(dir, fmt.Sprintf("cpu-%s-%s.prof", commandName, stamp))
	tracePath := filepath.Join(dir, fmt.Sprintf("trace-%s-%s.out", commandName, stamp))

	cpuFile, err := os.OpenFile(cpuPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", cpuPath, err)
	}

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		_ = cpuFile.Close()

		return nil, fmt.Errorf("starting CPU profile: %w", err)
	}

	traceFile, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		pprof.StopCPUProfile()
		_ = cpuFile.Close()

		return nil, fmt.Errorf("creating %s: %w", tracePath, err)
	}

	if err := trace.Start(traceFile); err != nil {
		pprof.StopCPUProfile()
		_ = cpuFile.Close()
		_ = traceFile.Close()

		return nil, fmt.Errorf("starting trace: %w", err)
	}

	return func() {
		trace.Stop()
		pprof.StopCPUProfile()

		if err := cpuFile.Close(); err != nil {
			slog.Warn("Failed to close CPU profile.", "error", err)
		}
		if err := traceFile.Close(); err != nil {
			slog.Warn("Failed to close trace.", "error", err)
		}

		slog.Debug("Profiles written.", "cpu", cpuPath, "trace", tracePath)
	}, nil
}
