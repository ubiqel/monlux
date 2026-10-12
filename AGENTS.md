# AGENTS.md

Linux-only Go CLI (`github.com/ubiqel/monlux`) that controls external monitor brightness over DDC/CI. Talks to `/sys/class/drm`, `/sys/bus/i2c/devices` and `/dev/i2c-N` directly.

## Commands

Tasks are defined in `Taskfile.yml` (run with `task <name>`):

- `task test`: `gotestsum -- -race ./...`. Single test: `go test -race -run TestName ./internal/monitor`.
- `task lint`: `golangci-lint run ./...` (config in `.golangci.yaml`).
- `task format`: `gofumpt --extra -w ./`.
- `task build.release`: writes `build/monlux`. `build/` is gitignored.
- `task deps`: `go mod tidy` + `go mod vendor`. It needs network. `vendor/` is committed and used (`modules-download-mode: vendor`), so after any dependency change re-vendor and commit `vendor/`.
- `go run ./cmd/monlux --debug <subcommand>` runs with debug logging and writes CPU profile and trace files to `$XDG_CACHE_HOME/monlux/debug/` (`os.UserCacheDir`).

## Lint state

`golangci-lint run ./...` and `gofumpt --extra -l cmd internal` are both clean. Keep them that way: fix new findings in your change rather than suppressing them.

- Project-wide exclusions live in `.golangci.yaml`
- Use an inline `//nolint:<linter> // reason` only for a single, isolated case (e.g. G103 on the ioctl call). If a pattern repeats, add a config exclusion instead.

## Layout

- `cmd/monlux/`: the `package main` CLI on `urfave/cli/v3`. Only CLI wiring and output formatting live here.
  - `main.go` wires commands, the global `--debug` flag, the lock and profiling.
  - `flags.go` defines flags shared by several subcommands.
  - `cmd_*.go`: one file per subcommand. Each one reads its arguments and calls into `internal/`. Logic specific to one subcommand (e.g. `cycle`'s step parsing) stays in its `cmd_*.go` file.
- `internal/monitor/`: finds monitors and selects one. Everything it touches outside the process is in `Host` (see Gotchas).
  - `monitor.go`: `Host`, `DefaultHost()`, `Monitor`, `ScanResult`, `BusReport`, and the real EDID/DDC hooks.
  - `discovery.go`: DRM connectors and candidate I2C buses from sysfs.
  - `scan.go`: `Host.Scan()`, plus connector association and conflict resolution. `ScanResult` gives per-bus reasons, which `detect` prints.
  - `cache.go`: `Host.Monitors()`, the cached monitor list.
  - `display.go`: `Host.Resolve` (`<monitor>` argument matching), `Host.WithDisplay` (open bus, run action, DPMS error wording), `Host.DPMSState`.
- `internal/brightness/`: reads and sets brightness.
  - `brightness.go`: `Current`, `Write`, `Observe` (cache-aware), and `Clamp`.
  - `cache.go`: brightness cache entries, TTL, and recent-write rules.
- `internal/i2c/`: DDC/CI and EDID over the I2C ioctl. `Open(bus)` opens `/dev/i2c-N`. Also VCP (brightness) access and checksum code.
- `internal/cache/`: file storage under `$XDG_CACHE_HOME/monlux/` only (`Path`, `ReadJSON`, `WriteJSON`, `ErrNotExist`). It has no cache rules of its own.
- `internal/lock/`: exclusive `flock` on `$TMPDIR/monlux.lock`, taken in `main`'s `Before` hook for every invocation.
- `internal/profile/`, `internal/log/`, `internal/version/`: debug profiling, slog handler, version string.

## Gotchas

- **Lock serializes everything.** Every command holds the global lock, so concurrent `monlux` calls queue. A hung I2C transaction blocks all others.
- **Writes hit real hardware.** `set`, `up`, `down` and `cycle` change the actual monitor, even when you're only experimenting. Use `list` or `get` to inspect.
- **Monitor IDs.** The `<monitor>` argument defaults to a DRM connector name (e.g. `card1-DP-2`). Prefixes `drm:`, `edid:`, `i2c:` select other identity types. Multiple matches are an error, and you must pick one with `i2c:<bus>`.
- **Cycle steps.** `--steps` must be strictly ascending or strictly descending, with no duplicates. The first two entries set the direction. Forward moves to the nearest higher step, and backward to the nearest lower one. Past either end, it wraps to the opposite end.
- **Caches can return stale values.**
  - Monitor list is cached in `$XDG_CACHE_HOME/monlux/monitors.json` for 60 s (`internal/monitor`). Scans that find no monitors aren't cached.
  - Brightness is cached in `brightness.json` for 10 s, keyed by I2C bus and EDID identity (`internal/brightness`).
  - When debugging brightness or detection, add `--no-brightness-cache`, or delete the files under `$XDG_CACHE_HOME/monlux/`.
- **Failures on a sleeping display.** `Host.WithDisplay` reports DPMS "asleep" when a DDC operation fails. Check the display state before debugging the I2C code.
- **Hardware is injected through `monitor.Host`.** It holds the sysfs paths (`DRMPath`, `I2CSysPath`, `DevPath`) and the hardware hooks (`ReadEDID`, `ProbeDDC`). Production code uses `monitor.DefaultHost()`. There are no package globals to swap.
- **Tests never touch real hardware.** `internal/monitor/fixture_test.go` (`installFixture`) builds a fake sysfs and `/dev` tree in a temp dir and returns a `Host` wired to fake buses. Keep new tests hermetic the same way. Brightness tests pass a `-1` fd, so any real I/O fails.
- **Cache tests isolate the cache.** Call `t.Setenv("XDG_CACHE_HOME", t.TempDir())` yourself. `installFixture` doesn't set it. `t.Setenv` panics if the test or any parent calls `t.Parallel()`, so tests that touch the cache must stay sequential (see `isolateCache` in `internal/brightness/cache_test.go`).
- **Tests run serially by default.** Most tests finish in well under a millisecond, so `t.Parallel()` only adds noise. Add it only when a test blocks for a long time (e.g. sleeps or real I/O waits) and no shared state or `t.Setenv` is involved.
- Tests use `github.com/matryer/is`.
