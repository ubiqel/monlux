# monlux

[![CI](https://github.com/ubiqel/monlux/actions/workflows/ci.yml/badge.svg)](https://github.com/ubiqel/monlux/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/ubiqel/monlux)](LICENSE)

Control external monitor brightness over DDC/CI on Linux.

`monlux` talks to `/sys/class/drm` and `/dev/i2c-N` directly, so it needs no external tools such as `ddcutil`.

## Requirements

- Linux
- A monitor with DDC/CI enabled in its on-screen menu
- The `i2c-dev` kernel module (`sudo modprobe i2c-dev` if `/dev/i2c-*` is missing)
- Read and write access to `/dev/i2c-*` (see [Permissions](#permissions))

## Installation

### Release archive

Download the archive for your architecture (`amd64` or `arm64`) from the [releases page](https://github.com/ubiqel/monlux/releases), extract it, and put `monlux` on your `PATH`.

### Go

```sh
go install github.com/ubiqel/monlux/cmd/monlux@latest
```

### From source

```sh
git clone https://github.com/ubiqel/monlux
cd monlux
task build.release   # writes build/monlux
```

## Usage

```sh
monlux list                      # detected monitors and their brightness
monlux get <monitor>             # print brightness, e.g. 25
monlux set <monitor> <percent>   # set brightness
monlux up <monitor> <step>       # raise brightness
monlux down <monitor> <step>     # lower brightness
monlux cycle <monitor>           # cycle through 0, 50, 100
```

`<monitor>` is a DRM connector name, as printed by `monlux list` (e.g. `card1-DP-2`). Run `monlux --help` or `monlux <command> --help` for details.

## Troubleshooting

Start with `monlux detect`. It explains, per I2C bus, which monitor was found and why others were skipped.

If a command fails on a monitor that is off or asleep, `monlux` reports it as asleep. Check the display's power state before debugging further.

## Permissions

`monlux` needs read and write access to `/dev/i2c-*`. Use one of these:

- **Desktop login (typical):** distros that ship the `ddcutil` or `i2c-tools` udev rules tag I2C devices with `uaccess`. systemd-logind then grants access to the user of the active local session. Check with `getfacl /dev/i2c-*`: the entry `user:<you>:rw-` means access is granted. `ls -l /dev/i2c-*` shows a `+` on devices that have an ACL. This does not apply over SSH or outside a local session.
- **`i2c` group:** add your user to the group and log in again:

  ```sh
  sudo usermod -aG i2c "$USER"
  ```

  This needs a rule that makes the group own the devices:

  ```udev
  # /etc/udev/rules.d/99-i2c.rules
  SUBSYSTEM=="i2c-dev", KERNEL=="i2c-[0-9]*", GROUP="i2c", MODE="0660"
  ```

  Then run `sudo udevadm control --reload && sudo udevadm trigger`.

## Caching

To avoid slow DDC/CI round trips, `monlux` caches results under `$XDG_CACHE_HOME/monlux/`:

- The monitor list is cached for 60 seconds.
- Brightness is cached for 10 seconds, per I2C bus and monitor identity.

Use `--no-brightness-cache` on `get`, `set`, `up`, `down`, or `cycle` to read and write the monitor directly. Or delete the files in that directory.

## Development

```sh
task test      # go test with the race detector
task lint      # golangci-lint
task format    # gofumpt
task --list    # all tasks
```

## License

[MIT](LICENSE) © Kirill Salnikov
