# AGENTS.md

Linux-only Go CLI (`github.com/ubiqel/shine`) that controls external monitor brightness over DDC/CI. Talks to `/sys/class/drm`, `/sys/bus/i2c/devices` and `/dev/i2c-N` directly.

## Commands

Tasks are defined in `Taskfile.yml` (run with `task <name>`):

- `task test`: `gotestsum -- -race ./...`. Single test: `go test -race -run TestName ./cmd`.
- `task lint`: `golangci-lint run ./...` (config in `.golangci.yaml`).
- `task format`: `gofumpt --extra -w ./`.
- `task build.release`: writes `build/shine`. `build/` is gitignored.
- `task deps`: `go mod tidy` + `go mod vendor`. It needs network. `vendor/` is committed and used (`modules-download-mode: vendor`), so after any dependency change re-vendor and commit `vendor/`.
- `go run ./cmd --debug <subcommand>` runs with debug logging and writes CPU profile and trace files to `$XDG_CACHE_HOME/shine/debug/` (`os.UserCacheDir`).

## Current lint state

`golangci-lint run ./...` already fails on the existing tree (about 100 findings, mostly `paralleltest`, `forbidigo`, `revive`, `gosec`). `gofumpt -l -extra` also flags `cmd/helpers.go` and `internal/profile/profile.go`. Don't fix these unprompted. Judge your change by whether it adds new findings.

## Layout

- `cmd/`: the `package main` CLI on `urfave/cli/v3`. One file per subcommand (`cmd_*.go`). `main.go` wires commands and the global `--debug` flag.
- `cmd/discovery.go`, `cmd/monitors.go`: scan sysfs and DRM connectors, and match I2C buses to monitors via EDID and DDC probes. `scan()` returns a `ScanResult` with per-bus reasons.
- `cmd/brightness_cache.go`: brightness read/write cache, see below.
- `internal/i2c/`: EDID and VCP (brightness) access over the I2C ioctl, plus checksum code.
- `internal/lock/`: exclusive `flock` on `$TMPDIR/shine.lock`, taken in `main`'s `Before` hook for every invocation.
- `internal/profile/`, `internal/log/`, `internal/version/`: debug profiling, slog handler, version string.
- `scripts/bench-list.sh`: times `build/shine list`.

## Gotchas

- **Lock serializes everything.** Every command holds the global lock, so concurrent `shine` calls queue. A hung I2C transaction blocks all others.
- **Display IDs.** `DISPLAY` defaults to a DRM connector name (e.g. `card1-DP-2`). Prefixes `drm:`, `edid:`, `i2c:` select other identity types. Multiple matches are an error, and you must pick one with `i2c:<bus>`.
- **Caches can return stale values.**
  - Monitor list is cached in `$XDG_CACHE_HOME/shine/monitors.json` for 60 s.
  - Brightness is cached in `brightness.json` for 10 s, keyed by I2C bus and EDID identity.
  - When debugging brightness or detection, add `--no-brightness-cache`, or delete the files under `$XDG_CACHE_HOME/shine/`.
- **Failures on a sleeping display.** `withDisplay` reports DPMS "asleep" when a DDC operation fails. Check the display state before debugging the I2C code.
- **Tests never touch real hardware.** `scan_fixture_test.go` (`installFixture`) builds a fake sysfs and `/dev` tree in a temp dir and swaps the `drmPath`, `i2cSysPath`, `devPath`, `readEDID` and `probeDDC` globals. Cache tests use `t.Setenv("XDG_CACHE_HOME", ...)`. Keep new tests hermetic in the same way.
- Tests use `github.com/matryer/is`.
