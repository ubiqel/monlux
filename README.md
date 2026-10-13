# monlux

[![CI](https://github.com/ubiqel/monlux/actions/workflows/ci.yml/badge.svg)](https://github.com/ubiqel/monlux/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/ubiqel/monlux)](LICENSE)

Control external monitor brightness over DDC/CI on Linux.

## Motivation

I built it to control monitor brightness from [Waybar](https://github.com/Alexays/Waybar). `ddcutil` does the job, but it has no caching, so each call waits on a slow DDC/CI round trip. Wrapping it in a bar module ends up messy. `monlux` caches brightness and monitor lookups, so repeated calls from a bar stay fast.

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

`<monitor>` identifies one monitor. It accepts:

- `card1-DP-2`: a DRM connector name, as printed by `monlux list`. Connector names can change after a reboot or when you switch ports.
- `drm:<connector>`: the same as above, with the type spelled out.
- `edid:<identity>`: the EDID identity from `monlux list`. Stays the same across reboots and ports.
- `i2c:<bus>`: the I2C bus number, e.g. `i2c:4`. Use it when several monitors share an EDID identity.

Run `monlux --help` or `monlux <command> --help` for details.

## Waybar

This is how I use `monlux` in [Waybar](https://github.com/Alexays/Waybar): scroll to change brightness, click to cycle through steps. The monitor is addressed by EDID identity, which stays the same across reboots and ports, unlike connector names.

```jsonc
"custom/monitor_brightness": {
    "format": "󰃠: {}%",
    "exec": "monlux get 'edid:<identity>'",
    "exec-on-event": false,
    "interval": 5,
    "signal": 8,
    "on-click": "monlux cycle 'edid:<identity>' --steps=0,50,100; pkill -SIGRTMIN+8 waybar",
    "on-click-right": "monlux cycle 'edid:<identity>' --steps=100,50,0; pkill -SIGRTMIN+8 waybar",
    "on-scroll-up": "monlux up 'edid:<identity>' 5; pkill -SIGRTMIN+8 waybar",
    "on-scroll-down": "monlux down 'edid:<identity>' 5; pkill -SIGRTMIN+8 waybar",
    "tooltip": false
}
```

The `signal` value is the real-time signal offset: `8` means `SIGRTMIN+8`. The `pkill` after each command makes Waybar re-read the value right away, rather than waiting for the next `interval`. Replace `<identity>` with the value from the EDID column of `monlux list`, keeping the `edid:` prefix.

## Limitations

- Laptop built-in panels are skipped, since they don't support DDC/CI.
- Only brightness is controlled. Other settings such as contrast or input source are not supported.
- Monitors need DDC/CI turned on in their on-screen menu.

## Troubleshooting

Start with `monlux detect`. For each I2C bus, it shows the EDID found on it, the linked DRM connector, whether DDC/CI answers, and why a bus was skipped.

Common errors:

- `no monitor matches "<id>"`: no monitor has that identifier. Compare it with `monlux list`, and check the prefix for `edid:` and `i2c:` forms.
- `matches N monitors`: several monitors share the same EDID identity. Select one with `i2c:<bus>`.
- `display is asleep`: the monitor is in DPMS sleep and didn't respond. Wake it and retry. `monlux list` shows `asleep` for the same state.
- `?` in the brightness column of `monlux list`: the monitor was found, but its brightness couldn't be read. Run `monlux detect`.
- `permission denied` when opening `/dev/i2c-N`: see [Permissions](#permissions).

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

Pass `--no-cache` to any command to skip both caches: it scans for monitors and reads and writes brightness directly, without saving anything. Or delete the files in that directory.

## License

[MIT](LICENSE) © Kirill Salnikov
