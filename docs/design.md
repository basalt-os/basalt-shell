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
| `drivers.state` | any | Additional drivers: the report basalt-drivers-refresh.service writes, read with `basalt drivers --json --cached` (GPUs, the driver that fits, its state, the NVIDIA license text), or `{"coming_soon": true}` with an assistant older than `basalt drivers` |
| `drivers.propose`, `drivers.rollback` | ui | store the assistant's driver.install proposal, or the rollback to the snapshot taken before it, for the confirmation step |
| `updates.state`, `channels.state` | any | Updates and channels: the assistant's `basalt updates --json` and `basalt channels --json`, or `{"coming_soon": true}` with an assistant older than them |
| `updates.check` | ui | update.check: the assistant's unit basalt-updates-check.service refreshes the package lists and the report (nothing is installed) |
| `updates.restart` | ui | the restart into a staged offline update, when the power menu's countdown ends: the assistant's unit basalt-offline-reboot.service |
| `updates.propose`, `updates.rollback`, `channels.propose` | ui | store the assistant's update.install, update.rollback, repo.enable, repo.disable, source.add or source.remove proposal for the confirmation step |
| `keyboard.state`, `keyboard.layouts`, `keyboard.indicator` | any | Settings, Keyboard: the person's settings, the system's keyboard, the layouts in use; every layout and variant of the XKB registry for the picker; the panel's indicator (labels, the active one) |
| `keyboard.set`, `keyboard.switch` | ui | save and apply the person's keyboard (checked against the XKB registry); switch to the next layout or to one |
| `keyboard.system` | ui | store the assistant's keyboard.system proposal (the login screen, the console, new accounts) for the confirmation step |
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
| `session.power` | op: lock, logout, suspend, restart, poweroff (the person only: see Session) |

Actions marked for the person only (`text.insert`, `mail.send`,
`files.move`, `voice.answers.set`, `session.power`) are planned only from
the person's own words in the command bar or by voice and confirmed in
the shell; they are not MCP tools and an agent's proposal of them is
refused. `session.power` may also be run by the shell UI's power menu,
after its own confirmation.

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

### With the approval gate (Basalt OS)

On Basalt OS the approval gate (basalt-gate, the basalt-os repository's
`docs/gate.md`) is the one place where a request becomes a decision. When
it is installed, the daemon asks the gate which approval paths it decides
(its `hello` answer, checked again every few seconds):

- where the gate decides (`shell` for desktop and person actions), a new proposal becomes a gate request. The daemon
  (`basalt_shell_t`, one of the gate's trusted relays) asks on behalf of
  the person (the command bar, push to talk) or of an agent (MCP and IPC
  clients, named after the client), with the shell's own plan as the
  preview (summary, steps, exact previews of acting steps, the token
  diff). A rule may allow or refuse it at once; otherwise the sheet, the
  command bar or the voice card shows it as before, and the person's
  decision goes from the shell UI (`basalt_shell_ui_t`, the gate's
  desktop decider) straight to the gate, never through the daemon, which
  refuses to decide such a proposal itself. The daemon waits for the
  gate's decision, claims it with exactly the calls and preview it is
  about to run (the gate compares them with what was approved), runs it
  and reports the result. A confirmation within 1.5 s of synthetic input
  is not trusted: the request is asked again. When the gate offers it,
  the sheet has "Approve and remember". The activity log names who
  decided: `gate:person:shell`, `gate:rule:<id>@<hash>`. Editing an
  e-mail draft makes the edited draft the request before the approval.
  Person-only actions stay person-only: the gate's registry refuses them
  from anyone but the person, before any rule.
- where it does not (shadow mode), the shell decides exactly as before
  and tells the gate what was decided (`observe`), so the gate's dry runs
  have real data before the switch.
- skill grants (`skills`): the grant request is `grant.folder`,
  `grant.mailbox` or `grant.site` at the gate (each names its folder,
  mailbox or host, so a person's rule can pre-approve exactly that). The
  person's approval is kept by the gate as a rule that ends with the
  grant: asking again for the same scope before then is allowed without a
  new question. Revoking the grant (End, or "stop reading") removes the
  rule at once.
- model downloads (`models`): the card's Download asks the gate with the
  card's consent (what, size, from where) as the preview, and the shell UI
  approves it there; the daemon claims it and starts the download as
  before (basalt-models-request still applies the administrator's
  `models.conf` and polkit).
- knowledge packs and remote content (`consent`): the typed actions
  `knowledge.fetch` and `remote.consent` carry the consent text as their
  step; the assistant loop (ADR 0018, 0019) proposes them on behalf of
  the assistant (tainted: it reads the web) or the person's words do;
  never an agent connection.
- agent control sessions and screenshots stay with the shell in this
  version (observed only).
- without a gate, nothing changes.

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
Apply starts the assistant's unit `basalt-apply@ID_CODE.service`
(`systemctl start`), so the person authenticates (an administrator's
password, in the shell's own polkit dialog) and the assistant's executor
runs `basalt apply ID --yes --confirm CODE`: it re-checks that the code
matches the commands, takes snapshots, runs, verifies and audits. The
shell never applies system changes itself and never runs dnf, rpm or
basalt apply in its own domains.

Where the approval gate decides the system assistant's proposals
(`apply`), Apply first queues the proposal in the gate (`assistant-read
submit ID`, which runs `basalt submit ID --json` as root: queueing changes
nothing), then the shell UI approves it at the gate (an administrator's
password through polkit, in the shell's own dialog), the gate starts the
assistant's executor (`basalt-gate-exec@REQUEST.service`, which applies
it with its snapshots, checks and audit record), and the shell follows
the request to its result. No pkexec of `basalt apply` then.

### Additional drivers

Settings, Additional drivers (pt-BR "Drivers adicionais") shows what
`basalt drivers --json --cached` reports (the report the assistant's root
unit basalt-drivers-refresh.service writes when the page opens; the shell
never runs rpm or dnf, and internal/assistant/policy_test.go refuses them
in the read helper): every display controller by PCI id and
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
and applies it like any other proposal (the assistant's unit
`basalt-apply@ID_CODE.service`, an administrator's password). GPUs that
need NVIDIA's 580 legacy driver (Maxwell, Pascal, Volta, for example a GTX 1070) get an explanation and a link: the
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

### Updates and channels

Settings, Updates and channels (pt-BR "Atualizações e canais") shows
what the system assistant reports (`basalt updates`, `basalt channels`;
the basalt-os repository's `docs/updates.md`) and changes nothing by
itself: every button stores one of the assistant's proposals through the
read helper and shows it on a sheet, in plain words, with the exact
commands under Details; the person confirms it, and it is applied like
any other proposal: where the approval gate decides the assistant's
proposals, through the gate and its executor (basalt-gate-exec@.service);
otherwise the shell starts the assistant's unit
`basalt-apply@ID_CODE.service` and polkit asks for an administrator's
password in the shell's own dialog. The unit runs `basalt apply ID --yes
--confirm CODE` in the assistant's executor (basalt_apply_t). The shell's
domains never run dnf, rpm or basalt apply and never write a repository
file; a test (internal/assistant/policy_test.go) fails if the shell's
policy gains an rpm transition. Checking for updates starts
`basalt-updates-check.service` (no password for an administrator at the
computer); the page reads the report the assistant keeps.

An update set that replaces core packages (the kernel, systemd, dbus,
glibc, the package manager, the SELinux policy, Basalt's shell, login
screen and gate, Mesa, the compositor) installs offline: the page says
so in one sentence and its button is "Restart and update"; the packages
are downloaded and prepared, the computer restarts, dnf installs them
before the session starts, and the assistant records the result with the
snapshot after it (basalt-offline-finish.service). The restart never comes
without warning: once the update is staged, the power menu opens on its 60
second countdown ("Restarting to install updates", focused on Cancel);
when it ends, or with Restart now, the daemon starts
basalt-offline-reboot.service (`updates.restart`). Cancel keeps the update
staged and the page offers "Restart and update" again. Smaller sets without
core packages install live. Both take a snapshot before and after and can
be undone.

- Updates: Check for updates (update.check, the last check's time), the
  updates grouped and explained (security updates highlighted, Basalt OS
  components, apps, system), with sizes and advisories behind "Show the
  list"; Install updates or Security updates only (update.install: a
  snapshot first; while it runs the page follows its steps); "Restart to
  finish" with a Restart now button when an update needs it; the history,
  with "Undo the last update" (update.rollback, back to the snapshot taken
  before it, at the next start). Automatic security updates are shown off:
  they become a rule of the approval gate once it runs scheduled jobs.
- Channels: a card per Basalt channel (what it is, who it is for, the
  risk, how it is signed: the OpenBasalt release key in short form) with a
  toggle where the person may change it. basalt is always on; basalt-nonfree
  is turned on by Additional drivers; basalt-nonfree-testing shows on
  hardware the NVIDIA driver supports, or after "Show all channels".
  Turning on a testing channel shows the consent sheet: "Preview builds can
  break things. A snapshot is taken before each update so you can go back."
  A driver channel that is not defined here yet needs basalt-nonfree-release:
  until the drivers report says it is installed or offered by the enabled
  repositories (`state.release_package`, `state.nonfree_available`, from
  dnf's cached metadata), its toggle is disabled and the card says "Not
  available yet: its packages are not published."
- A change that did not work ends with one line in plain words (not
  published yet, the sources could not be reached, a failed signature
  check, a full disk, the approval not given, else "Nothing was changed, or
  not everything worked"); the assistant's report, with the raw dnf
  output, stays behind "Show the assistant's report" (`shell/updates.js`,
  with its tests). The assistant stops at the first failed command.
- Other software sources: the catalog of well-known sources (Flathub, RPM
  Fusion, Google Chrome, Visual Studio Code, Docker CE) with the address
  and key Basalt OS pins, a COPR project by name, or a custom source by the
  address of its `.repo` file or its address and key address (https only;
  the page refuses plain http before asking). The sheet says that software
  from a source can change the whole system, shows the key's fingerprint
  and owner, and says whether Basalt OS knows the source. Added sources
  have their own cards (on and off, Remove, when they were added and who
  decided); repositories added another way are listed, not changed.
- A link to "How updates are verified" (the basalt-os repository's
  `docs/security/updates.md`).

Requests that store proposals are refused right after agent input, like
every other confirmation, and each is in the activity log. A request in
natural language reaches only the reports (`basalt updates`, `basalt
channels`); checking and proposing start from the page.

## Keyboard

Settings, Keyboard (pt-BR "Teclado") is the person's keyboard, kept in
`~/.config/basalt/keyboard.conf` (next to voice-and-assistant.conf, only
the daemon writes it) and applied to the running session at once:

- Layouts: a list, the first is the default. "Add a layout" searches every
  layout and variant of the system's XKB registry by name (translated
  with xkeyboard-config's own catalog, with a few clearer names such as
  "Portuguese (Brazil, ABNT2)" and "English (US, international with dead
  keys)"), its English name or its code; Return takes the first match.
  Each row moves up or down or goes away. While the person has not
  changed them the list shows the system's layouts; "Use the system's
  layouts" goes back to them. At most four (XKB's limit).
- Switch layouts: Super+Shift+Space always (a compositor key binding),
  plus one XKB combination if the person wants it (Alt+Shift, Ctrl+Shift,
  both Alt keys). The panel shows the layout in use ("BR", "US"; a layout
  listed twice gets its position, "US1") when there is more than one; a
  click or Return on it switches to the next.
- Try typing: a field to type in, and which layout is in use.
- Caps Lock (Caps Lock, Ctrl, Escape, swapped with Escape, off), the
  compose key (none, right Alt, Menu, right Ctrl, Caps Lock) and key repeat
  (delay 150 to 1000 ms, 10 to 80 per second), as XKB options and the
  compositor's repeat settings.

Every value is checked against the XKB registry the system ships
(`/usr/share/X11/xkb/rules/evdev.xml` and `evdev.extras.xml`): a layout,
variant or option that is not there is refused before anything reaches
the compositor (internal/keyboard). The system's own XKB options that
these settings do not own (`terminate:ctrl_alt_bksp`) are kept.

How it reaches the session. sway: `input type:keyboard xkb_variant "",
xkb_layout "br,us", xkb_variant ",intl", xkb_options ..., repeat_delay,
repeat_rate` over IPC (the lists quoted: sway splits commands on commas;
the variant is cleared first, so every step compiles), and the same in a
start-up file the shipped config includes before the person's own
additions (`~/.config/basalt-shell/keyboard/sway.conf`), so the next
session types with them from the first key; an input block in
`sway.d/` still wins. niri: the managed include `basalt-keyboard.kdl` next
to the niri config (basalt-session creates it empty), which niri reloads
at once. When the person uses the system's layouts the start-up files do
not name any, so a later change of the system's layouts reaches them.
Virtual keyboards (wtype, an agent's input) keep the keymap their client
gives them, so the indicator reads a real keyboard (sway GET_INPUTS, niri
KeyboardLayouts) and follows input and KeyboardLayout events.

The login screen and new accounts. The system's keyboard is what
systemd-localed keeps (`/etc/X11/xorg.conf.d/00-keyboard.conf` and the
console's `/etc/vconsole.conf`); the login screen and every new session
start from it. "Use my layouts there too" stores the system assistant's
`keyboard.system` proposal (`basalt keyboard set br,us(intl) --options
... --json` through the read helper, which only stores it): the sheet
opens on Not now, says what changes in plain words and shows the exact
commands under Details. The person applies it like any other proposal:
where the approval gate decides the assistant's proposals, through the
gate (class C2, a system change) and its executor; otherwise
basalt-apply@ID_CODE.service and an administrator's password in the
shell's polkit dialog. The executor runs `localectl set-x11-keymap
--no-convert LAYOUTS MODEL VARIANTS OPTIONS` and `localectl set-keymap
--no-convert KEYMAP` (the console keymap localed would pick: kbd's
converted keymap of the first layout, else systemd's kbd-model-map), then
checks localed's files. The shell never runs localectl or talks to
systemd-localed (internal/assistant/policy_test.go refuses it in the
shell's code, session scripts, helper and policy). People who chose their
own layouts keep them.

One source for the layouts shown everywhere. The page, the panel's
indicator and the lock screen's layout chip read the daemon's keyboard
state (`keyboard.indicator`: the person's layouts, or the system's when
they chose none, labelled "BR", "US", "US1" and "US2" for a layout listed
twice, and the active one from a real keyboard) and switch through it;
the lock screen asks the compositor itself only while the daemon does not
answer. The lock screen runs inside the session, so it types with the
person's layouts; its chip switches them. The login screen has no person:
it shows and types with the system's keyboard (localed's file, the
page's "Login screen and new accounts" section), with the same labels.

First start. The installer writes the keymap the person chose: Basalt's
installer the console keymap (`/etc/vconsole.conf`, `KEYMAP=br-abnt2`),
a kickstart's `keyboard` command both localed files. The session and the
login screen use the X11 keymap when there is one, else the layout of the
console keymap (`br-abnt2` and `br` give `br`, `uk` gives `gb`, otherwise
the part before the first hyphen), so the first session already types
with the chosen keyboard and there is nothing to do; the page shows those
layouts as the system's. A keymap whose layout this mapping cannot tell
(a console keymap with a variant, such as `us-acentos`, gives plain `us`),
or a computer installed with US, is fixed from the page: the person's own
layouts at once, and "Use my layouts there too" for the login screen,
which also writes the X11 keymap, so the mapping is no longer needed.

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

Every normal window can be closed with the mouse, and maximized and
minimized where the compositor allows it, on sway (SwayFX) and niri.

Who draws the title bar (package `internal/decor`): the app, whenever it
can draw a proper one with buttons (client-side decorations, CSD), in
the theme's colors:

- GTK 4 / libadwaita, GTK 3 headerbars, Firefox, Chromium and Electron
  ask for it themselves.
- GTK 3 windows without a header bar (Mousepad, older apps): the session
  sets `GTK_CSD=1`, so GTK 3 draws its own title bar (adw-gtk3) instead
  of asking for the compositor's.
- foot: Basalt's foot settings (`~/.config/foot/basalt-theme.ini`,
  included by a foot.ini the daemon creates only when there is none)
  say `[csd] preferred=client`, with the title bar and flat buttons in
  the theme's colors and the title in GTK's header size and weight
  (rewritten on every theme change).
- Qt asks for server-side decorations whenever the compositor offers
  them, so the daemon switches a new Qt window to client-side when the
  Adwaita decoration plugin of its Qt version is installed
  (`qt6-qtwayland-adwaita-decoration`, `qadwaitadecorations-qt5`, both
  required by the desktop edition; found through /proc/PID/maps and the
  plugin directories under /proc/PID/root, so Flatpak runtimes count
  too). The session sets `QT_WAYLAND_DECORATION=adwaita`.

So every app Basalt installs (foot, Files, Text Editor, Firefox) and
every GTK, libadwaita, Qt, Electron or Chromium app draws its own title
bar with buttons. Apps show only the buttons the compositor honors
(xdg_toplevel wm_capabilities, and GTK's button layout
`org.gnome.desktop.wm.preferences button-layout`, which the daemon sets
to match): sway 1.11 advertises only fullscreen and ignores maximize and
minimize requests, so close (`appmenu:close`); niri maximizes (to the
screen's edges, as a tiled column; the button again puts the window
back where it floated), so maximize and close (`appmenu:maximize,close`).

- sway draws a title bar for the rest: X11 apps, and tiled windows
  (sway grants client-side decorations only while a window floats, which
  is the default). sway cannot draw buttons on it, so the mouse gets
  there another way: a right click on it opens the window menu of that
  window, a middle click closes it (sway mouse bindings without
  `--whole-window` act only on title bars and frames, so clicks inside
  apps are untouched); a left click still drags. It is themed from the
  tokens: font (`font.family` SemiBold, one point under `font.size`),
  centered title, padding from `spacing.unit`, the focused title on
  `color.surfaceAlt` with `color.text`, inactive ones on
  `color.surface` with `color.textMuted`, a frame of `window.border`
  pixels in `color.border` (a little stronger when focused). Re-applied
  on every theme change, by the person or a model; a window that draws
  its own decorations never gets a `border` command (that would switch
  it back to server-side).
- niri has no title bars: every app is asked for client-side
  decorations (no `prefer-no-csd`) and xwayland-satellite gives X11
  apps a title bar with maximize and close. The focus ring is the
  accent.
- Everywhere, the panel's window list has a close button on the entry
  under the pointer, and its right click opens the window menu, so any
  window, whatever draws its title bar, can be closed, maximized or
  minimized with the mouse.

Why not draw buttons on sway's own title bars: sway has no buttons and
no way to add them from a config (patching SwayFX is a separate
decision). A layer of the shell drawing buttons over them was weighed
and left out: sway sends no event while a floating window is dragged or
resized, so buttons would trail or float away from their window; a
layer surface does not know the stacking of overlapping floating
windows, so a button would cover the window above; and every click on
it would have to be kept apart from the app below. With client-side
decorations for every toolkit that has them, sway's bar is left to X11
apps and tiled windows, where the right click menu and middle click
cover the same actions without any of those risks.

Window states (`window.set_state`), the same everywhere: maximize and
snap to a half are floating placements on the usable area (the shell
keeps the size before and reports `state` until the window is moved);
minimize uses sway's scratchpad, and on niri (no minimized state) parks
the window on a workspace named "minimized" at the end of the output,
hidden from the workspace list. The panel's window list shows the
windows of the visible workspace and the minimized ones: click focuses,
minimizes the focused one or restores a minimized one; middle click
closes; hovering an entry shows its close button; right click opens the
window menu (minimize, maximize, snap, float or tile, move to workspace
1 to 5, close; English and Brazilian Portuguese), also on a right click
on a sway title bar and with Super+Alt+Space. The menu opens under the
window's title bar, from its rectangle asked of the compositor at that
moment. Keys: Super+Up maximize or
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

New floating windows stay inside the usable area on sway: sway centers a
floating window on the whole output and lets it be as large as the
output, so a window as tall as the screen (IntelliJ IDEA restoring the
size it had, an X11 app asking for position 0,0) had its toolbar under
the panel. The daemon checks each new floating window in its first
seconds (at once, then after 0.4, 1.5 and 4 s, since X11 and Java apps
resize themselves after they map) and, when it does not fit the
workspace's usable area (the output minus the panel's exclusive zone),
shrinks and moves it inside. Fullscreen windows and windows that fit are
left alone.

The panel's window list never runs under the clock: entries shrink down
to their icon, and when even icons do not fit the list is clipped before
the clock and scrolls with the mouse wheel or a drag, keeping the
focused window's entry in view. Its entries stay keyboard stops of the
panel: Left and Right reach the ones scrolled out, and the list scrolls
to the entry with the keyboard focus.

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
| Idle and lock | swayidle runs basalt-lock after 15 minutes, before sleep and on logind's lock request; the shell's own lock screen (ext-session-lock-v1, PAM), swaylock in the theme's colors only as the fallback (below, Lock screen) |
| Autostart | basalt-session.target wants xdg-desktop-autostart.target |
| Flatpak apps | the session adds the Flatpak export directories to XDG_DATA_DIRS (login shells do it through profile.d; greetd starts no login shell), so the launcher and the panel find their launchers and icons, and those of RPMs whose scripts put icons there (Google Chrome) |
| Keyboard layouts | the session starts with the system's layouts (XKB_DEFAULT_* from /etc/X11/xorg.conf.d/00-keyboard.conf, as the login screen does), then the person's own from Settings, Keyboard (below); Super+Shift+Space switches to the next one |
| Java (JetBrains IDEs) | native Wayland by default in the 2026 IDEs; new windows are kept inside the usable area (above) |

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

### Power menu

The power button at the right end of the panel, the one in quick
settings and Super+Shift+E open a small menu: Lock screen, Log out,
Suspend, Restart, Power off (icons, keyboard: Up, Down, Return, Escape).
Log out, Restart and Power off ask first: a card counts down 60 seconds
and then goes ahead by itself (as GNOME does), with the button to do it
now and Cancel, and lists the open app windows so the person saves their
work. Lock and Suspend run at once. Typed or spoken requests ("restart
the computer", "reiniciar o computador", "shut down", "desligar", "log
out", "sair da sessão", "lock", "bloquear a tela", "suspend") become the
same action as a proposal the person confirms (the translator's intents
are `lock_screen`, `log_out`, `suspend`, `restart`, `power_off`).

Everything goes through logind from the person's session: `systemctl
suspend|reboot|poweroff` and `loginctl terminate-session`, with the
system's polkit rules (an active local session may restart or power off
without a password when nobody else is logged in; otherwise polkit asks
through the shell's dialog). Lock runs `basalt-lock`, as Super+L, and
the idle lock (swayidle) also answers logind's lock request. Without a
running shell UI, Super+Shift+E falls back to the compositor's own
confirmation (swaynag on sway, niri's quit dialog).

### Lock screen

Locking never shows a blank screen. The shell UI locks the session
through the compositor's session lock (ext-session-lock-v1, Quickshell's
WlSessionLock; sway and niri): from the first frame every output shows
the blurred wallpaper with the time and the date (shell/LockSurface.qml),
and the output that had the focus also shows the card, in the login
screen's look: the person's picture (AccountsService, else `~/.face`) or
initials, their name, and the password field with the keyboard already
in it and its hint, "Type your password to unlock" ("Digite sua senha
para desbloquear"). Under the field, one line says what the person
should know: Caps Lock (from the keyboard's LED, else guessed from the
typed letters), "That password did not work. Try again." after a refusal
(the card shakes and the field is cleared; after three, it points at
Caps Lock and the keyboard layout), "Too many attempts" when PAM says so,
and PAM's own messages (a fingerprint reader's prompt, an expiring
password). While PAM checks, the field says "Checking" and the button
spins. At the top right: the keyboard layout (press it to switch when
there are several), the network and the battery. The compositor gives
the keyboard to one lock surface (the first that appears, or the one
clicked), so keys typed on any output go into the card's field.

Keyboard: Return sends (only once something was typed), Escape clears
the field and the message, Tab moves between the field, the show
password and unlock buttons and the layout button, with the focus ring
on each (design rules above); nothing traps the focus.

Nothing from the desktop shows while locked: the compositor draws only
the lock surfaces, and they carry no notification text, window list or
preview. Volume keys work (niri's `allow-when-locked`); no other key
binding runs, so push to talk cannot start, and the daemon refuses it
anyway, with agent input and agent screenshots, while the UI reports the
lock (`ui.state`, which only the shell UI may send) or a locker program
runs.

Authentication is PAM's, in the UI process (Quickshell's PamContext,
service `basalt-lock`, `/etc/pam.d/basalt-lock`: system-auth, so
pam_unix checks the password through unix_chkpwd and a fingerprint
module added by authselect works through the same conversation); the
password goes from the field to PAM and nowhere else (never to the
daemon, never logged), and is cleared as soon as it is sent. The session
is unlocked in one place, after PAM's success for an answer the person
sent; the IPC target `lock` has only `lock` and `state`, there is no
unlock call, and shell/tests/lock.test.js checks both rules. While
locked the shell does not reload its files (an update installed meanwhile
waits until the unlock): rebuilding the lock surfaces under a held session
lock made Quickshell drop the lock in the lab, which the compositor turned
into its red "lock client gone" screen. The lock state is also kept in
PersistentProperties, should a reload happen anyway.

`basalt-lock` (Super+L, the power menu, swayidle on idle, before sleep
and for `loginctl lock-session`) asks the running shell UI (`ipc call
lock lock`), waits until the compositor confirms the lock (so the
computer never sleeps unlocked) and leaves a small guard: if the shell
UI stops while the screen is locked, the compositor keeps the session
locked and swaylock takes the abandoned lock over, so the person can
still unlock; the shell UI is started again after that. Without a
running shell UI, swaylock locks at once: the theme's colors over the
wallpaper, its indicator always visible (`--indicator-idle-visible`),
the keyboard layout, Caps Lock and failed attempts shown. Each lock is
written to the journal (`journalctl -t basalt-lock`).

Between the request and the first frame of the lock surfaces the
compositor shows a plain black screen (sway and niri blank every output
as soon as a session lock starts, before its surfaces arrive): about a
quarter of a second in the lab VM, from `loginctl lock-session` to the
clock and the field. If the shell UI dies, sway shows red and niri dark
red for about a second until swaylock takes over.

## Keyboard and focus

Every surface of the shell and the login screen works with the keyboard
alone, the same way everywhere, and shows where the keyboard is.

### The model

| Key | What it does |
|---|---|
| Tab, Shift+Tab | the next or previous group or control, in visual order (left to right, top to bottom) |
| Left, Right | inside a row of choices or actions (a group); at the start of a Settings row, Left goes back to the sidebar |
| Up, Down | inside a menu, a list, the Settings sidebar; Up and Down move a row in a grid (quick settings tiles) |
| Home, End | the first or last control of the group or menu |
| a letter | in the Settings sidebar and the power menu, the next entry that starts with it |
| Return, Enter, Space | press the focused control |
| Menu, Shift+F10 | the context menu of the focused entry (a window in the panel, a tray icon) |
| Escape | close the sheet, menu or surface and put the focus back on the control that opened it; in Settings, from the page back to the sidebar |
| Ctrl+W | close Settings |
| Ctrl+Alt+Tab, Super+B | the panel takes the keyboard (Escape gives it back) |

A group (a row of theme cards, of accent swatches, of options; a menu;
the Settings sidebar; the quick settings tiles) is one Tab stop: Tab
enters it on the selected control (or the one focused there last), the
arrows move inside it, Tab leaves it. This keeps Settings short to cross
with Tab and follows the roving tab stop of the WAI-ARIA patterns.

### Where the focus starts and where it goes back

- Settings opens on the current page's entry in the sidebar; Up and Down
  move along the sidebar and the page follows; Right, Return or Tab go
  into the page.
- Quick settings opens on its first tile, the power menu and the window
  menu on their first entry, the launcher and the command bar in their
  text field, the drawer on its tab.
- A surface opened from the panel or from quick settings goes back to
  the button that opened it when it is closed with Escape (power menu,
  then quick settings, then the panel). Opened from a key binding, it
  gives the keyboard back to the window that had it.
- Every surface that asks for approval opens with the focus on its
  negative action, with the ring showing, so a stray Return never
  approves anything: a confirmation sheet from an agent (also with
  "Approve and remember") on Decline; a proposal or a permission (a
  folder, a mailbox, a site) in the command bar on Ignore, Don't allow
  or Discard, and the system assistant's Apply on Ignore; the drivers
  confirmation in Settings on Cancel; a model or voice download offer
  on Not now; the screen-share chooser on Cancel; the power menu's
  countdown on Cancel. The polkit dialog opens on its password field,
  where Return submits only once something was typed. The arrows keep
  the visual order (Right or Left, Shift+Tab, reach the positive
  button). The positive buttons of a confirmation sheet take no input
  for 0.7 s after it appears (keys included); Escape declines.
- A card that asks something by itself (a model download offer) takes
  the keyboard only when no surface or sheet has it, with Not now
  focused and Escape as Not now.
- The focus is never lost to nowhere: every card is a focus scope, so
  when the focused button disappears (it hid after being pressed) the
  card keeps the keyboard and Escape and Tab still work.

### The focus ring

- 2 px wide, 2 px away from the control's edge, following its corners.
- Its color is the accent used as a foreground (`color.accentFg` when a
  theme defines it), moved toward white in dark mode or black in light
  mode until it has at least 3:1 against the page, panel and card colors
  (WCAG 2.2, 1.4.11 and 2.4.13), whatever accent the person picks
  (`Theme.focusRing`). High contrast on the login screen uses its yellow.
- It shows only for the keyboard: a click focuses a control without the
  ring, the next key shows it again (the focus-visible rule of the web,
  `Ui.focusVisible`). Text fields show it whenever they have the focus.
  The login screen is used with the keyboard first and shows it always.
- Scrolling areas that clip what is outside them (the activity feed, a
  long report, the NVIDIA license) draw it on their inner edge.

### Building a new screen

The behaviour lives in shared components (`shell/`), so a new screen
gets it by using them:

| Component | Use it for |
|---|---|
| `Pressable` | anything that does something when pressed (a tile, a swatch, a card, a menu row, a panel entry): Tab stop, Return and Space, context menu keys, focus ring, click focus without ring, screen reader role, name, description and checked state |
| `Btn` | buttons (a Pressable with icon, text and variants) |
| `Field` | one line of text: Tab stop, focus ring, name for screen readers; Escape goes on to the surface |
| `Slider` | numbers: Left, Right, Up, Down, Page Up, Page Down, Home, End; ring on the knob |
| `NavRow`, `NavFlow`, `NavColumn` | a group with one Tab stop and arrows (rows and wrapping rows of choices, menus and lists; `typeAhead` for letters) |
| `FocusRing` | the ring, for a control not built on Pressable |
| `Nav` | `initial` (focus a surface's first or named control when it opens), `groupKey` (arrows for any container: add `navRoving` and `tabStop`), `scrollKey`, `reveal` (scroll a focused control into view, done by the components) |
| `Surface` | cards: a focus scope; put the surface's Escape on it |

Rules for review:

1. No Rectangle with a MouseArea as a control: use Pressable or Btn.
2. Every icon-only control sets `accessibleName` (in the catalog, Tr.t);
   toggles and choices set `checkable` and `checked` or `active`.
3. Choices in a row go in a NavRow or NavFlow; menus and lists in a
   NavColumn. Each control in them is a direct child (a Repeater
   delegate counts).
4. A surface that takes the keyboard (`WlrKeyboardFocus.Exclusive` while
   open, never OnDemand: OnDemand only gets the keyboard after a click)
   calls `Nav.initial` when it opens and handles Escape on its Surface
   with `Ui.dismiss()` (or its own decline or cancel).
5. Open a surface from a control with `Ui.openFrom(surface, page, from,
   key)` (or `toggleFrom`), so Escape comes back to that control.
6. Never move the focus to a control the person did not reach (no
   focus theft from apps): surfaces take the keyboard only while they
   are open, and cards that ask by themselves wait for open surfaces.
7. Approval surfaces open focused on the negative action (Decline,
   Don't allow, Cancel, Not now), never on Confirm, Allow or Approve:
   a stray Return must never approve anything. A text field focused
   first ignores Return while it is empty.

### Tests

`lab/keyboard/run.sh` runs the shell in a container with sway headless
(Fedora's sway, the pixman renderer; SwayFX needs a GPU) and types with
wtype, as a person would, through the compositor: Settings (sidebar,
type-ahead, every group of the Appearance page, the slider, back to the
sidebar, the Voice and Additional drivers pages, Ctrl+W), quick settings
(tiles grid, themes, buttons, the power menu and back), the panel
(Ctrl+Alt+Tab, along it, the power menu and quick settings from it and
back), the window menu, the launcher, the command bar (a proposal
opens on Ignore and Return declines it; Left and Return apply one), an
agent's proposal (the sheet opens on Decline and Return right away
declines; Right to Confirm and Return confirms; Escape declines), the
screen-share chooser (opens on Cancel, Return cancels, Shift+Tab and
Return choose), the drawer's tabs, the Keyboard page (the picker, a layout
added, moved and removed, the switch key, the try field) and the panel's
layout indicator, in dark and light mode. After
each step the shell's IPC says which control holds the keyboard
(`ipc call shell focused`) and which surfaces are open (`surfaces`);
screenshots go to the output directory. `lab/greeter/e2e-fake.sh` does the
same for the login screen (the top bar and the language menu with the
keyboard alone, then the login), and `lab/lock/run.sh` for the lock
screen (two outputs, the real PAM stack: the field focused at once, a
wrong password, Escape, Caps Lock, the right password, the shell UI
killed while locked, the swaylock fallback).

## Packaging

`basalt-shell` RPM (spec in `packaging/`): daemon, client, UI launcher,
QML, themes, session files, portal configuration, polkit policy and
helper; `basalt-shell-selinux`: the `basalt_agent_base` and
`basalt_shell` modules. Every dependency (sway, Quickshell, niri,
xwayland-satellite, the portals, grim, wtype, wayvnc, qt5ct, qt6ct,
gnome-keyring-pam) is in Fedora 44's own repositories: no COPR.
