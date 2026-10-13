# AGENTS.md

Linux-only Go CLI (`github.com/ubiqel/monlux`) that sets external monitor brightness over DDC/CI. It reads `/sys/class/drm`, `/sys/bus/i2c/devices` and `/dev/i2c-N` directly. User docs are in `README.md`.

## Commands

Run `task --list` for all tasks. Needs `task`, `gotestsum`, `golangci-lint` and `gofumpt` on PATH.

- `task test`: race-enabled tests. Single test: `go test -race -run TestName ./internal/monitor`.
- `task lint`: `golangci-lint run ./...`. Config is `.golangci.yaml`.
- `task format`: `gofumpt --extra -w ./`. CI fails on any gofumpt diff.
- `task build.release`: writes `build/monlux`.
- `task deps`: tidy and vendor, needs network. `vendor/` is committed and used by the build, so after a dependency change re-vendor and commit it.
- `go run ./cmd/monlux --debug <subcommand>`: debug logs, plus CPU profile and trace in `$XDG_CACHE_HOME/monlux/debug/`.

Keep lint and gofumpt clean, since CI enforces both. Fix findings rather than suppressing them. Project-wide exclusions go in `.golangci.yaml`. Use an inline `//nolint:<linter> // reason` only for a single isolated case.

## Layout

- `cmd/monlux/`: CLI only (`urfave/cli/v3`). `main.go` wires commands, global flags, the lock and profiling. `cmd_*.go` has one file per subcommand. Domain logic goes in `internal/`.
- `internal/monitor/`: finds monitors and resolves selectors. `Host` holds every external dependency (sysfs paths, EDID/DDC hooks, `OpenBus`). Production code uses `DefaultHost()`.
- `internal/brightness/`: get, set, step and cycle over a `Device` interface, with a brightness cache.
- `internal/ddc/`: DDC/CI frames, retries and EDID over the `/dev/i2c-N` ioctl. `Bus.xfer` is the seam tests replace.
- `internal/cache/`: JSON files under `$XDG_CACHE_HOME/monlux/`.
- `internal/lock/`: global flock. `internal/profile`, `internal/log` and `internal/version` are support packages.

## Gotchas

- **The lock serializes everything.** Every invocation takes the global flock, so concurrent calls queue. One hung I2C transfer blocks all of them.
- **Writes hit real hardware.** `set`, `up`, `down` and `cycle` change the monitor. Use `list` or `get` to inspect.
- **Monitor selectors.** `<monitor>` defaults to a DRM connector such as `card1-DP-2`. The `drm:`, `edid:` and `i2c:` prefixes select other identities, and they are parsed in `cmd/monlux/monitor_arg.go`, not in `internal/`. More than one match is an error. Pick one with `i2c:<bus>`.
- **Caches go stale.** The monitor list is cached for 60 s in `monitors.json`. Brightness is cached for 5 s in `brightness.json`, keyed by I2C bus and EDID identity. When debugging detection or brightness, pass the root `--no-cache` flag (it works before or after the subcommand), or delete the files.
- **Sleeping displays look like DDC failures.** `Host.WithDisplay` reports "display is asleep" when a DDC operation fails on a display in DPMS sleep. Check the DPMS state before debugging I2C code.
- **Tests stay hermetic.** They never touch real hardware. Use `installFixture` (`internal/monitor/fixture_test.go`), `newScriptedBus` (`internal/ddc/bus_test.go`) or a fake `brightness.Device`.
- **Cache tests need isolation.** Call `t.Setenv("XDG_CACHE_HOME", t.TempDir())` yourself, since `installFixture` doesn't. `t.Setenv` panics under `t.Parallel()`, so cache tests stay sequential.
- Tests use `github.com/matryer/is`, not testify.

## Comments

Write a comment only to explain *why* something is done or to document a gotcha the code can't show. Don't restate what the code does or describe its signature. Keep existing `//nolint:` and `//go:` directives.
