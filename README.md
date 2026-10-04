# basalt-shell

> Prototype. Part of Basalt OS, a Fedora remix. Expect breaking changes.

The desktop shell of Basalt OS: a panel, an application launcher,
notifications, quick settings, an "Ask the system" command bar and an
activity feed, all drawn from one set of design tokens that anyone can
change live from a settings page. A model, local by default, can
coordinate the whole desktop through typed actions (windows, workspaces,
applications, theme, notifications, settings) over a local MCP server;
every change it asks for waits for the person's confirmation on a sheet in
the shell and is written to a hash-chained activity log.

The shell runs on sway / SwayFX and on niri through one adapter
interface, so the compositor can be swapped. Windows float by default;
tiling is one key away.

![The command bar proposing a theme change, on niri](media/niri/05-commandbar-proposal.webp)

![An MCP request waiting for the person, on SwayFX](media/swayfx/07-mcp-sheet.webp)

## Pieces

| Piece | What |
|---|---|
| `basalt-shell daemon` | Go, no third-party modules. Desktop state from the compositor adapter, design tokens and themes, the closed set of typed actions, proposals and confirmations, the audit log, the local socket, application appearance (GTK, libadwaita, Qt, portals) |
| `basalt-shell-ui` | Quickshell (Qt 6 / QML): panel, launcher, command bar, confirmation sheet, quick settings, notification center, activity feed, settings window, polkit dialog, wallpaper |
| `basalt-shell mcp` | MCP server on stdio for a local model or any MCP client |
| `basalt-shell ctl`, `basalt-shell propose` | the same from scripts |
| `basalt-session sway or niri` | starts a session; login entries "Basalt (SwayFX)" and "Basalt (niri)" |

How it works and why: [docs/design.md](docs/design.md). What the
prototype measured, SwayFX against niri, and the recommendation:
[docs/prototype-report.md](docs/prototype-report.md).

## Try it

Pick one.

1. In a window of your current Wayland desktop, nothing installed on your
   system (podman, Mesa GPU):

   ```sh
   scripts/try-podman.sh sway     # or: scripts/try-podman.sh niri
   ```

   The first run builds a Fedora 44 image with SwayFX, niri and Quickshell
   (a few minutes). Close the window (or Super+Shift+E) to stop.

2. Installed on Fedora 44 or newer (SwayFX from its COPR, the rest from
   Fedora):

   ```sh
   scripts/install.sh             # or --no-swayfx for Fedora's sway
   scripts/try-nested.sh sway     # a nested window first, if you like
   ```

   Then log out and pick "Basalt (SwayFX)" or "Basalt (niri)".

3. As part of Basalt OS: the installer's desktop profile
   (`basalt.profile=desktop`, see `lab/`) installs it with greetd.

Keys: Super+Space launcher, Super+A command bar, Super+S quick settings,
Super+N notifications, Super+Comma settings, Super+Return terminal,
Super+T float or tile, Super+Q close, Super+1..5 workspaces.

Things to type in the command bar: "make it darker with rounder corners",
"light mode", "use the lichen theme", "accent green", "bigger text",
"arrange windows side by side", "open text editor", "move firefox to
workspace 2", "reduce motion", and with the Basalt OS system assistant
installed: "why nginx", "disk", "snapshots".

## Let a model drive it

```json
{ "mcpServers": { "basalt-shell": { "command": "basalt-shell", "args": ["mcp"] } } }
```

Read tools: `desktop_state`, `theme_get`, `apps_list`, `activity_recent`,
`proposal_status`. Write tools: `window_focus`, `window_close`,
`window_move`, `window_set_floating`, `window_to_workspace`,
`windows_arrange`, `workspace_switch`, `app_launch`, `theme_set_tokens`,
`theme_switch`, `theme_reset`, `motion_set`, `notification_show`,
`settings_open`, `shell_open`. A write tool returns after the person
confirms or declines.

From a script:

```sh
basalt-shell ctl desktop
basalt-shell propose theme.set_tokens '{"tokens": {"radius.md": 16}}' --wait 60
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
make rpm                      # basalt-shell RPM in a Fedora container (podman)
```

## Layout

```
cmd/basalt-shell/       daemon, MCP server, ctl
internal/compositor/    adapter interface; sway (i3 IPC), niri, fake (tests)
internal/theme/         tokens, themes, settings, color math
internal/shell/         typed actions, proposals, IPC server, command bar, launch
internal/mcp/           MCP server and IPC client
internal/intent/        command bar understanding: rules, optional local model
internal/assistant/     bridge to the Basalt OS system assistant
internal/appearance/    gsettings, GTK css, qt6ct
internal/audit/         hash-chained log
shell/                  Quickshell QML
themes/                 Basalt, Lichen, Tide
config/                 sway and niri configs, sessions, portals, polkit, systemd
bin/, libexec/          session scripts, assistant read helper
lab/                    desktop profile for the installer, lab VM
scripts/                install, try (podman, nested), RPM, development
media/                  screenshots and recordings from the lab
```

## License

Code: Apache License 2.0 ([LICENSE](LICENSE)). Wallpapers from the Basalt
OS branding kit: CC-BY-SA-4.0 ([LICENSE-artwork](LICENSE-artwork)).
Quickshell is LGPL-3.0, SwayFX and sway MIT, niri GPL-3.0; they are
separate programs the shell runs on, not part of this repository.
