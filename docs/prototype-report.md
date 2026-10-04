# Prototype report: the Basalt shell on SwayFX and niri

Date: 2026-10-03. Prototype 0.1.0. How it is built: [design.md](design.md).

Superseded in part (0.2.0, 2026-10-04): the owner decided sway as the
default compositor (functionality and the API for models first), niri as
an optional session and no SwayFX; the confirmation check moved from the
program name to SELinux domains (selinux.md); qt5ct and the keyring
unlock at login closed two gaps of the matrix below.

## What was built

- A shell of our own on Quickshell (Qt 6 / QML): panel (workspaces, focused
  window, clock, "Ask" button with a badge for waiting proposals, tray,
  network, audio, battery, privacy indicator, notifications, quick
  settings), launcher with fuzzy search, notification server with popups
  and a notification center, quick settings, an "Ask the system" command
  bar, an activity feed, a settings window, confirmation and choice sheets,
  a polkit agent and a themed wallpaper.
- A Go daemon (no third-party modules) that owns every change: compositor
  adapters for sway / SwayFX and niri, design tokens with three themes,
  15 typed actions with proposals that wait for the person, a hash-chained
  audit log, a local socket with a ui/agent role split, an MCP server, the
  command bar's understanding (rules, or a local model when configured),
  the bridge to the system assistant, and application appearance (gsettings,
  the appearance portal, GTK css, qt6ct).
- Packaging and sessions: `basalt-shell` RPM, login entries "Basalt
  (SwayFX)" and "Basalt (niri)", portal configuration per compositor, a
  desktop profile for the Basalt installer (traditional Fedora; the server
  default is unchanged).
- A lab: the desktop ISO installed into a VM with a virtual GPU, demo
  scripts that drive the session with synthetic input, screenshots and
  recordings on both compositors, and measurements.

Unit tests cover the theme files (including WCAG contrast of every theme
and mode), the command bar rules, proposals (confirmation, decline, stale
refusal, validation), the IPC role split, the sway IPC client against a
fake socket and the MCP server end to end.

## Lab setup

- Fedora 44 base (Basalt OS 0.0.1 packages), installed from the Basalt
  installer with `basalt.profile=desktop`, plain btrfs, greetd with
  autologin of the lab user.
- VM with 4 vCPU and 8 GiB. virtio-gpu with virgl, rendered by the host's
  GTX 1070 through QEMU's egl-headless display. Under libvirt this fails
  with the NVIDIA driver (QEMU runs in a private /dev and a device cgroup
  that let it open the render node but not /dev/nvidia*), so the lab VM
  runs under a QEMU started by the lab user (`lab/run-gl-vm.sh`), with user
  networking and no TPM. The first install ran under libvirt with a 2D
  virtio-gpu (llvmpipe in the guest).
- Versions: swayfx 0.5.2 (COPR, an fc43 build in the f44 chroot, on
  wlroots 0.19.3), niri 26.04 (first 25.11, upgraded during the lab),
  Quickshell 0.2.1 (git dacfa9d, Fedora package), Qt 6.10.2, GTK 4.22,
  libadwaita 1.9, xdg-desktop-portal 1.21.1 with -gtk 1.15.3, -wlr 0.8.1,
  -gnome 50.0, xwayland-satellite 0.8.1, Xwayland 24.1.9, Firefox 157.

## Demos (both compositors)

Screenshots and recordings are in `media/swayfx/` and `media/niri/`.

| Scene | What it shows | Files |
|---|---|---|
| Panel | floating windows, workspaces, clock, status, tray; then "cascade windows" from the command bar | `01-panel-floating.png`, `01-panel.png` |
| Launcher | fuzzy search, keyboard navigation, launch in a systemd scope | `02-launcher-*.png`, `02-launcher.webm` |
| Notifications | `notify-send` popups (normal, critical), notification center | `03-*.png`, `03-notifications.webm` |
| Settings and themes | quick settings, Appearance page, Basalt / Lichen / Tide, light and dark, Design tokens, Motion, Assistant and AI pages | `04-*.png`, `04-settings-themes.webm` |
| Command bar | "make it darker with rounder corners": proposal with the token diff, Apply, live change of shell, windows and apps | `05-*.png`, `05-commandbar-theme.webm` |
| System assistant | "why nginx": the assistant's report and proposal, Apply, the shell's polkit dialog, `basalt apply --yes --confirm`, nginx active again | `06-*.png`, `06-assistant-proposal.webm` |
| AI through MCP | an MCP client calls `theme_set_tokens`; the confirmation sheet shows who asks and the diff; Confirm; the tool returns "applied"; activity feed | `07-*.png`, `07-mcp-confirm.webm`, `07-mcp-result.txt` |
| Regular apps | GNOME Text Editor, Mousepad, FeatherPad, KeePassXC, Firefox, xterm, xeyes, arranged in a grid by the command bar | `08-apps.png` |
| Compatibility | file chooser, screenshot and screen-share portals, tray menu, keyring prompt, Electron, HiDPI | `media/compat/` |

The MCP client in the demo is a scripted one (`lab/demo/mcp-call.py`): it
stands in for a model. Any MCP client works the same way.

## Measurements

From `lab/demo/measure.sh`, VM with virgl, motion on:

| | SwayFX 0.5.2 | niri 26.04 |
|---|---|---|
| Idle CPU, compositor / Quickshell / daemon (10 s) | 0.0 % / 0.0 % / 0.0 % | 0.0 % / 0.0 % / 0.0 % |
| Memory (RSS), compositor | 78 to 92 MiB | 107 MiB |
| Memory (RSS), Quickshell | 226 to 233 MiB | 241 MiB |
| Memory (RSS), basalt-shell daemon | 13 to 14 MiB | 13 MiB |
| Frames delivered during the same shell animation script (3 x drawer and launcher, about 11 s) | 152 | 85 |
| `basalt-shell ctl desktop` round trip, process start included | 12 to 13 ms | 13 ms |

The frame counts come from wf-recorder, which receives a frame only when
the screen changes; they say both compositors keep up with the shell's
animations in the VM, but they are not a frame-rate benchmark (niri also
damages less of the screen). By eye, over SPICE, the shell's animations
(200 to 320 ms) look smooth on both; niri adds its own window open, close
and move animations. Quickshell's memory (about 230 MiB) is the largest
cost of the shell.

Without 3D (the first, 2D VM): SwayFX ran on llvmpipe, the daemon detected
the software renderer (virtio-gpu without the VIRGL feature) and motion
"auto" turned the animations and shadows off; the shell was usable. niri
did not start at all: it skips software EGL renderers on purpose.

## Compatibility matrix

Tested in the lab VM on 2026-10-03. "yes" was seen working; "partly" has a
note.

| Check | SwayFX 0.5.2 | niri 26.04 |
|---|---|---|
| libadwaita (GNOME Text Editor, Files): launch | yes | yes |
| libadwaita: light/dark switch, live | yes (portal color-scheme) | yes |
| libadwaita: accent color, live | yes, named accent (GNOME settings backend, see note 1) | yes, named accent |
| libadwaita: exact palette from the theme | new windows (gtk-4.0/gtk.css, both modes) | new windows |
| GTK 3 (Mousepad): launch, light/dark | yes (adw-gtk3 / adw-gtk3-dark switched with the mode) | yes |
| Qt 6 (FeatherPad, Qt 6.10): theme palette | partly: qt6ct palette when the app starts, not live | partly: same |
| Qt 5 (KeePassXC 2.7): theme palette | no (needs qt5ct; Fusion default) | no |
| Firefox 157 (Wayland): launch, live dark/light | yes | yes |
| Electron (Element, Flatpak): launch on Wayland | yes (once the session exports its environment to systemd, note 2) | yes |
| X11 apps (xterm, xeyes) | yes, XWayland | yes, xwayland-satellite (started on demand) |
| Flatpak app (Element) with portals | yes | yes |
| File chooser portal (GTK 4 app) | yes (xdg-desktop-portal-gtk) | yes |
| Screenshot portal | yes (wlr) | yes with wlr; the GNOME backend asked for permission and then never answered (note 3) |
| Screen sharing in Firefox (PipeWire) | yes: output picked in the shell's chooser sheet (note 4) | yes: GNOME's chooser dialog |
| Panel privacy indicator while sharing | yes | yes |
| Clipboard Wayland to X11 and back | yes | yes |
| Tray (StatusNotifierItem: KeePassXC, nm-applet), menu | yes (menu checked on niri) | yes |
| Polkit agent (the shell's) | yes | yes |
| Secret service (gnome-keyring, gcr prompts) | partly (note 5) | partly (note 5) |
| Notifications to the shell | yes | yes |
| Idle lock and unlock (swaylock) | yes | yes |
| Autostart (XDG autostart through basalt-session.target) | yes (nm-applet), also after switching sessions (note 6) | yes, same |
| HiDPI | yes, scale 1.5 | yes, scale 1.5 and 2 |

Notes:

1. xdg-desktop-portal-gtk publishes color-scheme but not accent-color; the
   sway portal configuration now routes the Settings interface through the
   GNOME backend first. Accent colors in the portal are libadwaita's named
   set; the shell maps the theme's accent to the nearest one (terra roxa
   becomes "orange") and writes the exact color to gtk.css for new windows.
2. Apps launched by the shell run in their own systemd scope, so the user
   manager needs the session's environment (DISPLAY, the Wayland and Qt
   variables, the Electron Ozone hint): `basalt-session-init` exports it.
   Element asks which keyring to use; with the autologin of the lab the
   login keyring stays locked (see note 5).
3. With xdg-desktop-portal-gnome 50 on niri, the Screenshot request showed
   GNOME's permission dialog and then never sent a response, even with the
   permission stored. niri implements wlr-screencopy, so the niri portal
   configuration now sends screenshots to the wlr backend.
4. xdg-desktop-portal-wlr's suggested chooser (slurp) crashed in the lab,
   and choosing a screen is a privacy decision anyway: the shell now asks
   it on a sheet (`basalt-shell choose-output`, answer audited).
5. gnome-keyring works through D-Bus activation and gcr's prompter, but
   the login keyring is unlocked by PAM at login only with a greeter that
   runs pam_gnome_keyring; greetd's autologin does not, so apps that need
   secrets prompt, and a scripted prompt did not complete in the lab. For
   real logins: add pam_gnome_keyring to greetd's PAM stack (or use our
   own greeter, Quickshell has a greetd service).
6. A session ended by the greeter (not by the compositor exiting) left the
   systemd targets active, so autostarted apps of the next session were
   not started again; the session now restarts its targets at start and
   cleans up on TERM.

## SwayFX against niri

| | SwayFX | niri |
|---|---|---|
| Visuals | rounded corners, shadows, blur and dimming, all changed live over IPC; no window animations | rounded corners, shadows and a focus ring (live, through the managed include and niri's config reload); smooth window, workspace and overview animations |
| Animation in the VM | the shell's animations smooth; nothing from the compositor | smooth, including niri's own; needs a GPU |
| Weak hardware / no 3D | runs on llvmpipe; the shell turns motion off | does not start |
| IPC for AI control | complete: full tree with geometry of every window, any command, events; style at runtime; `exec` goes through sh (the shell launches apps itself) | complete enough for all 15 actions: windows, workspaces, outputs, floating move and resize, focus, close, spawn with argv, event stream; geometry only for floating windows; no runtime style (config reload instead) |
| Floating windows | mature; floating by default with one rule; tiling a key away | supported since 25.01 but secondary to scrollable tiling: floating by default works (one rule), dialogs and placement fine, no maximize for floating windows |
| Workspaces | numbered, created on demand | dynamic per output, always one empty at the end |
| X11 | XWayland built in | xwayland-satellite, started on demand, in Fedora |
| Packaging in Fedora 44 | COPR only (swayfx/swayfx): an fc43 build from July 2025 in the f44 chroot, pulling scenefx 0.4 from the COPR while Fedora ships scenefx 0.5 | official package in updates (26.04), plus xwayland-satellite |
| Maintenance risk | higher: a small team following sway with a lag; the COPR build is stale; plain sway is the fallback (same IPC, no eye candy) | lower for packaging (Fedora); upstream very active, one lead maintainer; frequent releases, config changes now and then (the adapter writes only a small include) |

## Recommendation

Make niri the default compositor of the desktop edition and keep sway as
the second backend:

- It gives the owner's "beautiful, subtle animations" out of the box, is
  an official Fedora package (no COPR in the default install, which the
  signed, traditional repository model of ADR 0005 and 0006 prefers), and
  its IPC covers every action the model needs.
- Floating by default works on niri with one window rule; scrollable
  tiling is a strength for people who later want tiling.
- niri needs a working GPU. For machines and VMs without 3D, and for
  people who prefer i3-style behavior, the session list keeps "Basalt
  (sway)": same shell, same actions. Use Fedora's plain sway there, or
  package SwayFX and scenefx in Basalt's own repository if its eye candy is
  wanted; do not depend on the COPR.
- Keep the adapter interface as the contract: the shell, the MCP tools and
  the command bar did not change between compositors.

Quickshell is a good fit for the shell layer: LGPL-3.0, packaged in Fedora
44, live QML, Wayland layer shell, and service modules for notifications,
tray, PipeWire, UPower, polkit, greetd and NetworkManager that replaced
whole daemons. Risks: it is pre-1.0 (we pin to Fedora's build and test on
upgrade), its memory use is about 230 MiB, and it renders through Qt's
scene graph, so effects need a GPU (the shell already avoids shaders and
drops shadows when motion is reduced).

## Trying it

- In the lab VM over the LAN (SPICE or VNC; details in the private access
  note), both sessions installed, switch with the greeter.
- `scripts/try-podman.sh sway|niri`: a nested session in a window of the
  current desktop, from a Fedora 44 container; nothing installed on the
  host. Tested inside the lab VM (a nested SwayFX in the niri session).
  Polkit and NetworkManager status do not work inside the container.
- `scripts/install.sh` then a login session, or `scripts/try-nested.sh`
  for a nested window with its own D-Bus bus, config and state (tested in
  the VM: the nested daemon drives the nested compositor and leaves the
  host's settings alone).

## Decisions for the owner

1. Default compositor of the desktop edition: niri (recommended) or keep
   Sway as decided in ADR 0003 (then SwayFX from our repository, or plain
   sway).
2. Toolkit for Basalt's own layer: Quickshell (Qt/QML), as prototyped.
3. Whether niri's need for a GPU is acceptable for the desktop edition
   (sway stays as the session for machines without 3D).
4. Greeter: tuigreet now; a Quickshell greeter later (themed like the
   shell), with pam_gnome_keyring so the keyring unlocks at login.
5. Whether to package SwayFX and scenefx in the Basalt repository.
6. Qt 5 apps: ship qt5ct too, or accept that Qt 5 apps keep Fusion colors.
7. Local model for the command bar: wire the existing basalt-llm
   translator (same endpoint) once the M2b model is picked; the rules
   cover the demo phrases today.

## Known gaps

- The ui role is checked by the peer's executable name; any process of the
  same user could still synthesize input. The product needs the agent
  processes confined (SELinux domain, as for the system assistant).
- Live palette changes reach GTK 4 and GTK 3 apps through color-scheme and
  the named accent only; the exact palette applies to new windows.
- No lock screen of our own yet (swaylock with the theme's colors).
- The launcher does not show recently used apps; no app grid.
- The command bar rules are English and Portuguese keywords, not a model.
