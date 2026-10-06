# Basalt shell design

Status: prototype (0.2.0). This document describes how the shell is put
together and why. Measurements, the compositor comparison and open
questions of 0.1.0 are in [prototype-report.md](prototype-report.md).
The confirmation boundary is in [selinux.md](selinux.md), the headless
session for agents in [headless.md](headless.md) and the command bar's
local model in [command-bar.md](command-bar.md).

Decided (2026-10-03, after the 0.1.0 comparison in
[prototype-report.md](prototype-report.md)): functionality and the best API for models
and MCP come before looks. The default compositor is upstream sway from
Fedora (runs without a GPU and headless, the most complete IPC, wlroots
protocols for screen capture, virtual input and toplevel lists); niri is
an optional second session (basalt-shell-niri). Since 2026-10-04 the sway
session runs on SwayFX where Basalt OS ships it (same i3 IPC).

## Goals

- An AI-native desktop: a model (local by default, or one the person plugs
  in) can coordinate the whole desktop through typed actions, never by
  driving the mouse, and never without the person's confirmation.
- Our own shell, beautiful and easy for end users to change: one set of
  design tokens, editable from a settings page, applied live.
- Compositor-agnostic: the shell talks to the compositor through one
  adapter interface, so the compositor can be swapped later. sway is the
  default and also runs headless, for agents in VMs and servers.
- What people expect from a desktop: windows float by default, tiling is
  optional; regular apps (GTK, libadwaita, Qt, Electron, X11, Flatpak)
  work and follow the theme.
- Subtle animations, with a reduced-motion setting, turned off
  automatically on weak hardware.

## Components

```
                 person                                   agents
                   |                                         |
   Quickshell UI (QML, role "ui")              basalt-shell mcp (stdio)    basalt-shell ctl / scripts
   panel, launcher, command bar,                       |                          |
   confirmation sheet, settings, ...                   +------------+-------------+
   SELinux: basalt_shell_ui_t                          |  role "agent", SELinux: basalt_agent_mcp_t
                   |                                                |
                   +---------------- Unix socket -------------------+
                                $XDG_RUNTIME_DIR/basalt-shell/shell.sock
                         (role from SO_PEERCRED + SO_PEERSEC of the peer)
                                          |
                               basalt-shelld (Go), SELinux: basalt_shell_t
        +----------------+----------------+-----------------+------------------+
        |                |                |                 |                  |
  compositor adapter  theme store    typed actions     audit log        appearance sync
  sway (i3 IPC)        tokens, themes, + proposals     (hash chain)     gsettings, portal,
  niri (JSON socket)   settings.json   (confirmation)                   GTK css, qt5ct, qt6ct
        |
  agents' last resort: grim (screencopy), wtype and the daemon's own
  virtual keyboard and pointer (wlvirt), behind confirmation and a
  visible control session
                                          |
                               system assistant bridge
                               (basalt CLI: read through a polkit helper,
                                apply through basalt apply --confirm)
```

- `basalt-shelld` (the daemon) is the single owner of desktop state and of
  every change. It has no third-party Go modules.
- `basalt-shell-ui` runs Quickshell (Qt 6 / QML) with the shell's QML
  through `basalt-shell-ui-launch`, the entry point of the UI's SELinux
  domain. The UI holds no logic that changes the desktop: it asks the
  daemon.
- `basalt-shell mcp` is an MCP server (protocol 2025-06-18, stdio). It is
  just another client of the daemon's socket, with the agent role.
- `basalt-shell ctl OP [JSON]` and `basalt-shell propose ACTION [JSON]` are
  the local IPC from scripts.

## IPC protocol

Newline-delimited JSON over a Unix socket (mode 0600, directory 0700).

```
-> {"id": 1, "op": "hello", "args": {"role": "ui", "client": "quickshell"}}
<- {"id": 1, "ok": true, "result": {"role": "ui", "version": "0.1.0"}}
-> {"id": 2, "op": "propose", "args": {"calls": [{"action": "theme.set_tokens", "args": {"tokens": {"radius.md": 14}}}], "wait": 120}}
<- {"id": 2, "ok": true, "result": {"id": "d-1a2b3c4d", "status": "applied", "steps": [...], "diff": [...]}}
<- {"event": "theme", "data": {...}}          (ui role only: desktop, theme, proposal, activity, notify, ui)
```

| Op | Who | What |
|---|---|---|
| `state`, `desktop`, `theme`, `apps`, `actions`, `activity`, `pending`, `proposal` | any | read |
| `propose` (optionally `wait`), `wait` | any | ask for typed actions; they run only after the person confirms |
| `execute` | ui | run actions the person started in the UI (a click, a slider) |
| `decide` | ui | confirm or decline a proposal |
| `ask` | ui | command bar request (understand, then propose) |
| `theme.save_as`, `reload` | ui | save the current look as a user theme, reload theme files |
| `choose` | any | ask the person to pick one of fixed options (the screen-share output chooser); waits for the answer |
| `chosen` | ui | the person's answer to a `choose` |
| `assistant.pending`, `assistant.show` | any | the system assistant's proposals |
| `assistant.apply`, `assistant.ignore` | ui | the system assistant's own confirmation flow |
| `drivers.state` | any | Additional drivers: the assistant's `basalt drivers --json` (GPUs, the driver that fits, its state, the NVIDIA license text), or `{"coming_soon": true}` with an assistant older than `basalt drivers` |
| `drivers.propose`, `drivers.rollback` | ui | store the assistant's driver.install proposal, or the rollback to the snapshot taken before it, for the confirmation step |
| `toplevels` | any | the windows (with their foreign-toplevel identifiers) |
| `capture` | agent | a screenshot for this agent: confirmed by the person, or inside its control session |
| `input` | agent | one synthetic input step (type, key, move, click, scroll), only inside the agent's control session |
| `control`, `control.stop` | any | the control session; anyone may stop it |
| `ui.state` | ui | the UI tells whether it shows a modal dialog |
| `audit.verify` | any | check the activity log's chain |

Roles. Every connection starts as `agent`. The `ui` role is granted only
to a peer of the same user (SO_PEERCRED) whose SELinux domain (SO_PEERSEC)
is the shell UI's, `basalt_shell_ui_t`, running Quickshell, and only to
one connection at a time. Agents (`basalt_agent_mcp_t` and the rest of
the agent family) can read and propose, never confirm. Without the Basalt
policy the daemon falls back to the program name and says so. Details,
tests and limits: [selinux.md](selinux.md). Writes from agents are always
proposals, whatever they claim to be.

## Typed actions

The closed set (16 kinds, plus 2 that only agents may propose: below).
Each has a JSON-schema of parameters, strict validation, a human summary,
and runs only through the daemon:

| Action | Parameters |
|---|---|
| `window.focus`, `window.close` | window (id, app id, title fragment or "focused") |
| `window.move` | window, x, y, width, height (floats the window) |
| `window.set_floating` | window, floating |
| `window.set_state` | window, state: normal, minimized, maximized, left, right (the shell remembers the size to restore) |
| `window.to_workspace` | window, workspace |
| `windows.arrange` | layout: grid, columns, rows, cascade, center, tile, float; workspace |
| `workspace.switch` | workspace |
| `app.launch` | app (installed desktop entry only; no free-form command) |
| `theme.set_tokens` | tokens {key: value}, mode |
| `theme.switch` | theme, mode (light, dark, toggle), reset |
| `theme.reset` | tokens (default: all) |
| `motion.set` | motion: auto, full, reduced |
| `notification.show` | summary, body, urgency |
| `settings.open` | page |
| `shell.open` | surface: launcher, commandbar, quicksettings, activity, notifications, settings |

Read tools for MCP: `desktop_state`, `toplevels_list`, `theme_get`,
`apps_list`, `activity_recent`, `proposal_status`, `agent_control_status`.
Write tools are the actions above, named with underscores (`window_move`,
`theme_set_tokens` and so on), and the last-resort tools below.

## Agents' last resort: screen and input

For applications without a typed action, the MCP server offers what
wlroots makes possible, always through the daemon (agents cannot reach
the Wayland socket themselves):

| Tool | Rule |
|---|---|
| `screen_capture` (`screen.capture`) | a PNG of an output or a window (grim, wlroots screencopy; a single window through ext-image-copy-capture where the compositor has it, else its area of the screen); each screenshot is confirmed on the sheet, which says the agent will see the screen; the image goes to that agent only (not to the log, not kept) |
| `agent_control_request` (`agent.control`) | a control session of 1 to 15 minutes, confirmed on the sheet with a warning; bound to the requesting process |
| `input_type_text`, `input_key`, `input_pointer_move`, `input_pointer_click`, `input_scroll` | only inside that session: text and keys through the virtual keyboard protocol (wtype), the pointer through the daemon's own virtual pointer (`zwlr_virtual_pointer_v1`, absolute positions in layout coordinates) |
| `agent_control_stop` | ends the session |

While a session runs, every screen has a frame and a banner saying which
agent controls the desktop, the time left and a Stop button; the panel
shows "Agent in control"; Super+Shift+Escape stops it. A screenshot
outside a session flashes a short banner. Input is refused while the
person has a confirmation, a choice or an authentication dialog open; a
confirmation that arrives less than 1.5 s after synthetic input is
refused, and the sheet's buttons take no input for 0.7 s after a request
appears, so an agent cannot click or type its own "Confirm". Every
capture and input step (with the text typed) goes to the activity log.

## Proposals and confirmation

A proposal is a list of action calls, planned against the current state:
every call is validated (unknown actions or parameters, values out of
range and unknown windows are refused), summarized, and theme changes are
computed into a token diff. Then it waits for the person:

- from an agent (MCP or IPC): a confirmation sheet over the desktop shows
  who asks, each step and the diff, with Decline and Confirm;
- from the command bar: the proposal appears in the bar with Apply and
  Ignore.

Proposals expire after 5 minutes. A theme proposal computed against
settings that have changed since is refused as stale instead of applying
something other than what the person saw. Applied, declined, expired,
refused and failed requests are all written to the audit log.

The MCP write tool blocks until the decision (default 180 s) and tells
the model plainly what happened ("declined: nothing changed, do not
retry without asking").

## Audit log

`$XDG_STATE_HOME/basalt-shell/audit.jsonl`: one JSON record per line with
sequence, time, type (`request`, `confirm`, `apply`, `decline`, `expire`,
`refuse`, `fail`, `ask`, `start`), actor (`ui`, `commandbar`,
`agent:mcp`, `agent:ctl`, `assistant`), text, data, the previous record's
hash and its own SHA-256. `basalt-shell audit verify` checks the chain.
The activity feed in the shell is this log.

## Command bar

Free text goes to the daemon (`ask`):

1. Fixed phrases (deterministic rules, English and Portuguese) first:
   darker or lighter, light or dark mode, rounder or sharper corners,
   bigger or smaller text, spacing, accent color, a theme by name, panel
   position, shadows, blur, motion, open an app, close, focus, move to a
   workspace, arrange windows, open settings. Clauses compose: "make it
   darker with rounder corners" is one theme proposal with both changes.
   When they understand the whole request they are used: exact and
   instant.
2. Anything else goes to the local language model when one is configured
   (the system assistant's `[translator]` section in
   `/etc/basalt/assistant.conf`, the same basalt-llm service as `basalt
   ask`). Its output is constrained to a closed set of desktop intents,
   checked against the words of the request and turned into the same
   typed actions. Details and measurements: [command-bar.md](command-bar.md).
3. System questions ("why nginx", "disk", "snapshots", "selinux denials",
   "status", "pending", "show p-1a2b3c") go to the system assistant; with
   the model, its own translator (`basalt ask`) picks the command.

The result is a proposal shown with its diff; nothing runs until Apply.

## System assistant bridge

The `basalt` assistant (basalt-os repository) keeps its state root-only.
The shell reads it through `/usr/libexec/basalt-shell/assistant-read`, run
with pkexec: it accepts only read commands (status, why UNIT, fix
selinux, disk, snapshots, pending, show ID, audit N). A polkit rule lets
local administrators (wheel, active local session) run it without a
password. When an answer contains a proposal, the shell shows the
assistant's own report with the exact commands and the confirmation code;
Apply runs `pkexec basalt apply ID --yes --confirm CODE`, so the person
authenticates (in the shell's own polkit dialog) and the assistant
re-checks that the code matches the commands, takes snapshots, runs,
verifies and audits. The shell never applies system changes itself.

### Additional drivers

Settings, Additional drivers (pt-BR "Drivers adicionais") shows what
`basalt drivers --json` reports: every display controller by PCI id and
the kernel driver bound to it, whether NVIDIA's list of supported GPUs
covers an NVIDIA GPU with the open kernel modules (Turing and newer), the
recommended driver (the NVIDIA driver of Basalt OS's opt-in basalt-nonfree
repository), what installing it changes (the repository, the packages,
nouveau off, the snapshot, the check at the next start, the kernel update
guard, the restart), the Secure Boot state, and on a laptop with two GPUs
that the integrated GPU stays the display GPU while programs use the
NVIDIA GPU through PRIME offload. The NVIDIA Driver License Agreement is
shown on the page and must be accepted before the Install button works;
GeForce and Titan GPUs also get the license's datacenter notice. Install
stores the assistant's `driver.install` proposal (through the read helper,
`drivers install nvidia --json`), shows its report with the exact commands
and applies it like any other proposal (pkexec, `basalt apply ID --yes
--confirm CODE`). GPUs that need NVIDIA's 580 legacy driver (Maxwell,
Pascal, Volta, for example a GTX 1070) get an explanation and a link: the
assistant will guide that path later; nothing is packaged for them.

Install shows only when the report says the basalt-nonfree repository's
definition can be installed (`state.nonfree_available`). While that
repository is not published (the assistant then reports the action
`unavailable`; an older report without the field counts the same), or
with an assistant older than `basalt drivers` (it answers `unknown
command "drivers"`, and `drivers.state` returns `{"coming_soon": true}`),
the page says that driver installation is coming soon, with no button and
no error.

After a failed first start the driver falls back to nouveau (basalt-nvidia
in Basalt OS). The daemon reads basalt-nvidia's state at the start of a
session and shows a notification that opens the page, which explains why
and offers the rollback to the snapshot taken before the install
(`drivers rollback --json`, then the same confirmation). Every request is
in the activity log.

## Design tokens

One flat, closed set of tokens (`internal/theme/tokens.go`), each with a
kind, range and group:

| Group | Tokens |
|---|---|
| color (per mode) | bg, surface, surfaceAlt, border, text, textMuted, accent, accentText, success, warning, danger, scrim |
| typography | font.family, font.mono, font.size, font.scale (modular type scale) |
| shape | radius.sm, radius.md, radius.lg, radius.window |
| spacing | spacing.unit (4 px grid), panel.height, panel.position, panel.opacity |
| elevation | elevation.shadow (strength), elevation.blur (softness) |
| motion | motion.fast, motion.normal, motion.slow (ms), motion.easing |
| windows | window.gaps, window.border, window.shadows, window.blur, window.dimInactive |
| apps | apps.iconTheme, apps.cursorTheme, apps.cursorSize, apps.palette (full, accent, off) |

A theme file (`themes/*.json`) has shared tokens and one color set for
light and one for dark. The person's settings
(`~/.config/basalt-shell/settings.json`) pick a theme, a mode and a motion
preference, and hold overrides (color overrides per mode). The resolved
set is: theme tokens, then the mode's colors, then overrides, then motion
(durations become 0 when motion is reduced). Unknown keys and values out
of range are rejected everywhere, so a model cannot invent a token.

Three sample themes: Basalt (ink and terra roxa, the brand), Lichen (moss
greens, rounder, calmer motion) and Tide (slate and sea blue, tighter
corners, panel at the bottom). A test checks WCAG contrast (4.5:1 for
text, 3:1 for muted text) for every theme and mode.

End users change tokens from Settings: Appearance (theme cards, light and
dark, accent swatches, corner roundness, text size, spacing, panel) for
everyone, Design tokens (every token, with its range, override marker and
reset) for the curious, and "Save as theme" writes the current look as a
new theme file. Themes are plain JSON and can be shared.

## Motion

Every animation in the QML binds its duration to a motion token. Motion
"auto" turns animations off when the hardware is weak: no GPU render node
(software rendering, as in a VM without 3D), very few CPUs or less than
3 GiB of memory (`internal/hw`); `BASALT_SHELL_WEAK=0|1` overrides it.
Reduced motion also turns off shadows drawn by the shell (expensive with
software rendering), the compositor's animations (niri) and GTK
animations (`enable-animations`).

## Compositor adapters

```go
type Adapter interface {
    Name() string; Version(ctx) string; Caps() Caps
    Windows(ctx) ([]Window, error); Workspaces(ctx) ([]Workspace, error); Outputs(ctx) ([]Output, error)
    Focus(ctx, id) error; Close(ctx, id) error; SetFloating(ctx, id, on) error
    MoveResize(ctx, id, Rect) error; MoveToWorkspace(ctx, id, Workspace) error
    SwitchWorkspace(ctx, Workspace) error; Spawn(ctx, argv) error
    ApplyStyle(ctx, Style) error; Subscribe(ctx) (<-chan Event, error)
}
```

- sway (the default): the i3 binary IPC on `$SWAYSOCK` (GET_TREE,
  GET_WORKSPACES, GET_OUTPUTS, RUN_COMMAND, SUBSCRIBE). Style through
  runtime commands: client colors, borders, gaps. sway 1.11 reports each
  window's foreign-toplevel identifier. Pointer fallback through `seat
  cursor` commands. On SwayFX the adapter also sets corners, shadows (also on client-decorated
  windows), blur and dimming of inactive windows from the tokens; on weak
  hardware (hw.Probe: software rendering, few CPUs, little memory, headless)
  shadows, blur and dimming stay off.
- niri: JSON requests on `$NIRI_SOCKET` (Windows, Workspaces, Outputs,
  Action, EventStream). niri has no runtime styling command, so the
  adapter writes `basalt-theme.kdl` next to the niri config (which
  includes it) and niri reloads it live: focus ring, corner radius,
  shadows, gaps, animations on or off, cursor.

`Caps` says what a backend can do so tools can be honest (for example
`config_reload` for niri, `live_corners` only on SwayFX among the sway
family). Applications are launched as transient services of the systemd
user manager (`app-basalt-<id>-<random>.service`, in `app.slice`), not
through the compositor, when a user manager runs. Not a scope: a scope
would run the program as a child of the daemon, in the daemon's SELinux
domain; a service is started by the user manager, in the person's own
domain (docs/selinux.md, "Applications the shell starts").

## Window decorations and window states

Who draws the title bar (package `internal/decor`): an app that can draw
a proper one with buttons does (client-side decorations): GTK 4 /
libadwaita, GTK 3 headerbars, Firefox, Chromium and Electron ask for it
themselves; Qt asks for server-side decorations whenever the compositor
offers them, so the daemon switches a new Qt window to client-side when
the Adwaita decoration plugin of its Qt version is installed
(`qt6-qtwayland-adwaita-decoration`, `qadwaitadecorations-qt5`; found
through /proc/PID/maps and the plugin directories under
/proc/PID/root, so Flatpak runtimes count too). The session sets
`QT_WAYLAND_DECORATION=adwaita`.

- sway draws a title bar for every other window (X11, terminals, plain
  GTK 3), themed from the tokens: font (`font.family` SemiBold, one point
  under `font.size`), centered title, padding from `spacing.unit`, the
  focused title on `color.surfaceAlt` with `color.text`, inactive ones on
  `color.surface` with `color.textMuted`, a frame of `window.border`
  pixels in `color.border` (a little stronger when focused). Re-applied
  on every theme change, by the person or a model; a window that draws
  its own decorations never gets a `border` command (that would switch
  it back to server-side).
- niri has no title bars: every app is asked for client-side
  decorations (no `prefer-no-csd`); X11 apps get the frame
  xwayland-satellite draws; foot's own title bar follows the theme
  (`~/.config/foot/basalt-theme.ini`, included by a foot.ini the daemon
  creates only when there is none). The focus ring is the accent.
- GTK's button layout (`org.gnome.desktop.wm.preferences button-layout`)
  shows only buttons the compositor honors: sway 1.11 advertises only
  fullscreen and ignores maximize and minimize requests, so
  `appmenu:close`; niri maximizes (to edges), so `appmenu:maximize,close`.
  GTK 4 and Qt's Adwaita plugin hide unsupported buttons on their own.

Window states (`window.set_state`), the same everywhere: maximize and
snap to a half are floating placements on the usable area (the shell
keeps the size before and reports `state` until the window is moved);
minimize uses sway's scratchpad, and on niri (no minimized state) parks
the window on a workspace named "minimized" at the end of the output,
hidden from the workspace list. The panel's window list shows the
windows of the visible workspace and the minimized ones: click focuses,
minimizes the focused one or restores a minimized one; middle click
closes; right click opens the window menu (minimize, maximize, snap,
float or tile, move to workspace 1 to 5, close), also on a right click
on a sway title bar and with Super+Alt+Space. Keys: Super+Up maximize or
restore, Super+Left / Super+Right snap, Super+Down restore or minimize,
Super+H minimize; focus moves with Super+Alt+arrows. The command bar
understands "minimize firefox", "maximize", "snap terminal to the left",
"restore text editor".

Mouse: drag a title bar or headerbar to move, Super+drag anywhere,
Super+right-drag to resize. sway: compositor title bars and frames
resize from their 2 px edge; client-decorated windows cannot be resized
from their edges (sway does not route the pointer outside a window's
geometry) and a double click on a headerbar does nothing. niri: edges
resize client-decorated windows and a double click maximizes.

Floating by default: sway `for_window [app_id=".*"] floating enable` (and
for X11 classes); niri `window-rule { open-floating true }`. Tiling stays
one key away (Super+T per window) and per workspace (`windows.arrange
tile`, the Windows settings page, or "tile windows" in the command bar).

## Regular applications

| Need | How |
|---|---|
| File chooser, app chooser | xdg-desktop-portal-gtk on both compositors |
| Settings portal (color-scheme, accent-color) | -gnome first, -gtk second (the GTK backend does not publish accent-color) |
| Screenshot | -wlr on both (on niri, -gnome 50 asked for permission and never answered) |
| Screen sharing (PipeWire) | -wlr on sway / SwayFX, the output chosen on a sheet in the shell (`basalt-shell choose-output`, audited); -gnome on niri (niri implements the Mutter screen cast API, with its own chooser) |
| Dark/light and accent for every app (Flatpak too) | org.freedesktop.appearance color-scheme and accent-color through the portal, from gsettings written by the daemon |
| Privacy | a panel indicator while any app captures the screen, a camera or the microphone (PipeWire input streams) |
| GTK 4 / libadwaita | the portal settings live; exact palette for both modes (libadwaita CSS variables under a prefers-color-scheme media query) in a managed block of ~/.config/gtk-4.0/gtk.css, read when an app starts |
| GTK 3 | adw-gtk3 / adw-gtk3-dark switched with the mode; the accent in a managed block of ~/.config/gtk-3.0/gtk.css |
| Qt 5 / 6 | qt5ct and qt6ct (QT_QPA_PLATFORMTHEME=qt6ct:qt5ct) with a generated color scheme and fonts, when the app starts |
| Apps with their own colors | KeePassXC's default theme ("Automatic") follows the portal's color-scheme when it starts; FeatherPad's text area ignores the palette, so the daemon sets its `darkColorScheme` to the mode in `~/.config/featherpad/fp.conf` (read when it starts; not when the `apps.palette` token is `off`) |
| Icons, cursor, fonts | gsettings (icon-theme, cursor-theme, cursor-size, font-name, monospace-font-name) and the compositor's cursor |
| X11 apps | XWayland on sway; xwayland-satellite on niri (started on demand by niri) |
| Electron | Ozone Wayland (ELECTRON_OZONE_PLATFORM_HINT=auto), dark mode from the portal |
| Polkit agent | the shell's own (Quickshell polkit service), themed |
| Secrets | gnome-keyring (Secret Service and the Secret portal), unlocked at login by pam_gnome_keyring in greetd's PAM stack (gnome-keyring-pam) |
| Tray | StatusNotifierItem host in the panel |
| Notifications | org.freedesktop.Notifications served by the shell |
| Clipboard | wl-clipboard, cliphist history |
| Idle and lock | swayidle and swaylock in the theme's colors (basalt-lock) |
| Autostart | basalt-session.target wants xdg-desktop-autostart.target |

## Session

`basalt-session sway|niri|headless` sets the toolkit environment and
starts the compositor with the shell's config; the wayland-sessions
entries "Basalt" (sway) and "Basalt (niri)" call it, and
`basalt-headless.service` runs the headless session. `basalt-session-init`, started by
the compositor, exports the session's environment to the systemd user
manager (apps run in their own scopes and need DISPLAY, the Wayland and
Qt variables and the Electron hint), stops portals left from an earlier
session and starts basalt-session.target, the clipboard history and the
idle lock. Local additions: `~/.config/basalt-shell/sway.d/*` (sway) and
`~/.config/basalt-shell/niri/local.kdl` (niri). When NVIDIA's kernel module
is loaded (the NVIDIA driver, even on a laptop where the integrated GPU
drives the display), sway and SwayFX start with `--unsupported-gpu`,
without which they refuse to start; niri needs no flag. The desktop profile of the Basalt
installer uses greetd with the Basalt login screen, basalt-greeter
([greeter.md](greeter.md)), and tuigreet as its text fallback. Run inside another desktop (a nested
window), the session does not export anything to the host's systemd user
manager and does not change the host's application settings.

## Packaging

`basalt-shell` RPM (spec in `packaging/`): daemon, client, UI launcher,
QML, themes, session files, portal configuration, polkit policy and
helper; `basalt-shell-selinux`: the `basalt_agent_base` and
`basalt_shell` modules. Every dependency (sway, Quickshell, niri,
xwayland-satellite, the portals, grim, wtype, wayvnc, qt5ct, qt6ct,
gnome-keyring-pam) is in Fedora 44's own repositories: no COPR.
