# basalt-shell

> Prototype. Part of Basalt OS, a Fedora remix. Expect breaking changes.

The desktop shell of Basalt OS: a panel, an application launcher,
notifications, quick settings, an "Ask the system" command bar and an
activity feed, all drawn from one set of design tokens that anyone can
change live from a settings page. A model, local by default, can
coordinate the whole desktop through typed actions (windows, workspaces,
applications, theme, notifications, settings) over a local MCP server;
every change it asks for waits for the person's confirmation on a sheet in
the shell and is written to a hash-chained activity log. Only the shell
UI's SELinux domain can confirm; agents run confined and can only ask.

The shell runs on sway (the default, from Fedora; also headless, without
a display or GPU, for agents in VMs and servers) and on niri, through one
adapter interface. Windows float by default; tiling is one key away.

![The command bar proposing a theme change, on niri](media/niri/05-commandbar-proposal.webp)

![An MCP request waiting for the person](media/swayfx/07-mcp-sheet.webp)

## Pieces

| Piece | What |
|---|---|
| `basalt-shelld` | the daemon. Go, no third-party modules. Desktop state from the compositor adapter, design tokens and themes, the closed set of typed actions, proposals and confirmations, the audit log, the local socket (peer checked by SELinux context), application appearance (GTK, libadwaita, Qt 5 and 6, portals), screen capture and virtual input for agents |
| `basalt-shell-ui` | Quickshell (Qt 6 / QML): panel, launcher, command bar, confirmation sheet, agent-control indicator, quick settings, notification center, activity feed, settings window, polkit dialog, wallpaper |
| `basalt-shell mcp` | MCP server on stdio for a local model or any MCP client (confined in `basalt_agent_mcp_t`) |
| `basalt-shell ctl`, `propose`, `screenshot` | the same from scripts |
| `basalt-session sway, niri or headless` | starts a session; login entries "Basalt" and "Basalt (niri)"; `basalt-headless.service` |
| `selinux/` | the `basalt_shell` policy module (docs/selinux.md), built on the agent family's `basalt_agent_base` |

How it works and why: [docs/design.md](docs/design.md). The
confirmation boundary: [docs/selinux.md](docs/selinux.md). A desktop for
agents without a display: [docs/headless.md](docs/headless.md). The
command bar's local model: [docs/command-bar.md](docs/command-bar.md).
What the 0.1.0 prototype measured (SwayFX against niri):
[docs/prototype-report.md](docs/prototype-report.md).

## Try it

Pick one.

1. In a window of your current Wayland desktop, nothing installed on your
   system (podman, Mesa GPU):

   ```sh
   scripts/try-podman.sh sway     # or: scripts/try-podman.sh niri
   ```

   The first run builds a Fedora 44 image with sway, niri and Quickshell
   (a few minutes). Close the window (or Super+Shift+E) to stop.

2. Installed on Fedora 44 or newer (everything from Fedora):

   ```sh
   scripts/install.sh
   scripts/try-nested.sh sway     # a nested window first, if you like
   ```

   Then log out and pick "Basalt" or "Basalt (niri)". The SELinux policy
   comes with the RPMs (`make rpm`).

3. Headless, for an agent: `systemctl --user start basalt-headless`
   ([docs/headless.md](docs/headless.md)).

4. As part of Basalt OS: the installer's desktop profile
   (`basalt.profile=desktop`, see `lab/`) installs it with greetd.

Keys: Super+Space launcher, Super+A command bar, Super+S quick settings,
Super+N notifications, Super+Comma settings, Super+Return terminal,
Super+T float or tile, Super+Q close, Super+Up maximize, Super+Left and
Super+Right snap, Super+Down restore, Super+H minimize (the panel's window
list brings it back), Super+Alt+Space window menu, Super+1..5 workspaces,
Super+Shift+Escape stop an agent's control session.

Things to type in the command bar: "make it darker with rounder corners",
"light mode", "use the lichen theme", "accent green", "bigger text",
"arrange windows side by side", "open text editor", "move firefox to
workspace 2", "reduce motion", and with the Basalt OS system assistant
installed: "why nginx", "disk", "snapshots".

## Let a model drive it

```json
{ "mcpServers": { "basalt-shell": { "command": "basalt-shell", "args": ["mcp"] } } }
```

Read tools: `desktop_state`, `toplevels_list`, `theme_get`, `apps_list`,
`activity_recent`, `proposal_status`, `agent_control_status`. Write tools:
`window_focus`, `window_close`, `window_move`, `window_set_floating`,
`window_set_state`, `window_to_workspace`, `windows_arrange`, `workspace_switch`,
`app_launch`, `theme_set_tokens`, `theme_switch`, `theme_reset`,
`motion_set`, `notification_show`, `settings_open`, `shell_open`. A write
tool returns after the person confirms or declines.

Last resort, for apps without a typed action: `screen_capture` (a PNG of
a screen or window, confirmed per screenshot) and
`agent_control_request` (a visible, time-limited control session,
confirmed by the person) with `input_type_text`, `input_key`,
`input_pointer_move`, `input_pointer_click`, `input_scroll` and
`agent_control_stop`.

From a script:

```sh
basalt-shell ctl desktop
basalt-shell propose theme.set_tokens '{"tokens": {"radius.md": 16}}' --wait 60
basalt-shell screenshot --window foot > foot.png
basalt-shell audit verify
```

## Make it yours

Settings (Super+Comma): theme, light or dark, accent color, corner
roundness, text size, spacing, panel position and opacity; every token on
the Design tokens page; "Save as theme" keeps the look as a theme file in
`~/.config/basalt-shell/themes/`. Themes are JSON: copy one from
`/usr/share/basalt-shell/themes/` and edit it. Local compositor additions
go in `~/.config/basalt-shell/sway.d/` (sway) or the copied niri config.

## Build

```sh
make build test               # Go 1.24 or newer
make install PREFIX=/usr/local
scripts/build-selinux.sh      # the SELinux module in a Fedora container (needs basalt_agent_base)
make rpm                      # basalt-shell and basalt-shell-selinux RPMs in a Fedora container
```

## Layout

```
cmd/basalt-shelld/      the daemon
cmd/basalt-shell/       MCP server, ctl (the agent side)
cmd/basalt-shell-ui-launch/  entry point of the UI's SELinux domain
internal/compositor/    adapter interface; sway (i3 IPC), niri, fake (tests)
internal/agentio/       screen capture (grim) and virtual keyboard (wtype)
internal/wlvirt/        the daemon's own Wayland virtual keyboard and pointer
internal/theme/         tokens, themes, settings, color math
internal/shell/         typed actions, proposals, IPC server, command bar, launch
internal/mcp/           MCP server and IPC client
internal/intent/        command bar understanding: rules, the local model
internal/assistant/     bridge to the Basalt OS system assistant
internal/appearance/    gsettings, GTK css, qt6ct
internal/audit/         hash-chained log
shell/                  Quickshell QML
themes/                 Basalt, Lichen, Tide
config/                 sway and niri configs, sessions, portals, polkit, systemd
selinux/                basalt_shell policy module
bin/, libexec/          session scripts, assistant read helper
lab/                    desktop profile for the installer, lab VM
scripts/                install, try (podman, nested), RPM, development
media/                  screenshots and recordings from the lab
```

## License

Code: Apache License 2.0 ([LICENSE](LICENSE)). Wallpapers from the Basalt
OS branding kit: CC-BY-SA-4.0 ([LICENSE-artwork](LICENSE-artwork)).
Quickshell is LGPL-3.0, sway MIT, niri GPL-3.0; they are
separate programs the shell runs on, not part of this repository.
