# opensnitch-zenity

A small prompt UI for the [OpenSnitch](https://github.com/evilsocket/opensnitch)
application firewall daemon. It replaces the Python/Qt `opensnitch-ui` with a
single Go binary that asks about connections using
[zenity](https://gitlab.gnome.org/GNOME/zenity) dialogs.

Unofficial; not affiliated with the OpenSnitch project. Tested against
opensnitchd **1.7.2** on Arch Linux with Hyprland and zenity 4.2.

## What it does and does not do

- Answers the daemon's connection prompts, and nothing else. There is no
  statistics view, rule editor or firewall editor; edit rules as JSON in
  `/etc/opensnitchd/rules/` (the daemon reloads them on change).
- Idle memory is about 20 MB RSS.

## The prompt

For each new connection the daemon asks about:

| Button | Result |
|---|---|
| **Allow once** / **Deny once** | Applies to this connection only. Closing the window = Deny once. |
| **Allow always** / **Deny always** | A second dialog asks what the rule covers: *program to any host*, *program to this host only*, *any program to this host*, or *Just once*. The daemon saves the rule to `/etc/opensnitchd/rules/`. |

Each dialog waits `prompt_timeout` (default 30 s). If nobody answers the first
dialog, `default_action` is applied **once**; if nobody answers the scope
dialog, the chosen action is applied once.

No dialog is shown, and `default_action` is applied once, when:

- neither `WAYLAND_DISPLAY` nor `DISPLAY` is set,
- your graphical session (logind) is not in the foreground or is locked
  (`LockedHint`), or
- `locked_check_command` exits 0 (for lockers that do not set `LockedHint`).

The daemon asks about one connection at a time and applies **its own**
`DefaultAction` (from `/etc/opensnitchd/default-config.json`) to everything
else while a dialog is open or when no UI is running. An identical connection
repeated within 2 s reuses the previous answer instead of prompting again.

## Install

```sh
sudo pacman -S --needed opensnitch zenity go
git clone https://github.com/vmartinv/opensnitch-zenity
cd opensnitch-zenity
make build
sudo make install          # /usr/bin + systemd user unit
systemctl --user enable --now opensnitch-zenity.service
sudo systemctl enable --now opensnitchd
```

Or build a package with `contrib/PKGBUILD` (`cd contrib && makepkg -si`).

Do not run the stock `opensnitch-ui` at the same time: only one UI can listen
on the socket. opensnitch-zenity refuses to start if another UI is already
listening.

## Configuration

`~/.config/opensnitch-zenity/config.toml` (all keys optional):

```toml
socket = "unix:///tmp/osui.sock"   # must match Server.Address in default-config.json
prompt_timeout = "30s"             # per dialog, 1s..55s
default_action = "deny"            # deny | allow | reject, used when nobody answers
notify_on_default = false          # notify-send when default_action is applied
log_level = "info"                 # debug | info | warn | error
zenity_path = "zenity"
locked_check_command = ""          # e.g. "pgrep -x hyprlock"
```

Flags override the file: `--config`, `--socket`, `--timeout`,
`--default-action`, `--log-level`.

Logs go to stderr: `journalctl --user -u opensnitch-zenity -f`.

## Things to know

- **Every user's connections are prompted in your session.** opensnitchd is
  system-wide; prompts include other users' processes (the dialog shows the
  user) and system services.
- **Your own tools prompt too.** Allow your terminal tools, package manager and
  browser early, or the dialogs will keep coming.
- **Socket security.** The socket is created mode 0600 in `/tmp`; the daemon
  runs as root and can still connect. Any local user could create
  `/tmp/osui.sock` first if no UI is running; use a path under
  `/run/user/$UID` in both configs if that matters on your machine.
- **Window rules.** zenity dialogs are normal windows. On Hyprland they float
  and center by default; otherwise match the window title `OpenSnitch`.

## Development

```sh
make test     # unit tests + integration tests over a real unix socket
make vet      # gofmt + go vet
make proto    # regenerate proto/*.pb.go after updating proto/ui.proto
```

`proto/ui.proto` is vendored unchanged (apart from `go_package`) from
OpenSnitch v1.7.2. Rule replies mirror what the stock UI sends
(`opensnitch/dialogs/prompt`). For a new daemon version, replace the proto,
run `make proto`, and check the daemon's `daemon/ui/client.go` and
`daemon/rule/rule.go` for changes.

Manual end-to-end check (keep the daemon's `DefaultAction` at `allow` while
experimenting so nothing gets cut off):

```sh
sudo systemctl start opensnitchd
./opensnitch-zenity --log-level debug --default-action allow
curl https://example.org                 # dialog appears; choose Allow always
sudo ls /etc/opensnitchd/rules/          # allow-always-...json saved by the daemon
sudo tail /var/log/opensnitchd.log       # "Saved new rule: ..."
```

## License

GPL-3.0-or-later. See [LICENSE](LICENSE).
