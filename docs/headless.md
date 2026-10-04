# A headless Basalt desktop for agents

sway runs without a display, a GPU or input devices (wlroots' headless
backend with the pixman CPU renderer), with the whole Basalt shell on it:
an agent in a VM or on a server drives a real desktop through the shell's
MCP server, and a person watches and confirms through a VNC view.

## Start it

As the user the agent works for (the user manager must run: log in once,
or `sudo loginctl enable-linger USER` on a server):

```sh
systemctl --user start basalt-headless        # or: basalt-session headless
```

`basalt-headless.service` runs `basalt-session headless`, which:

- starts sway with `WLR_BACKENDS=headless`, `WLR_LIBINPUT_NO_DEVICES=1`
  and one output of `BASALT_HEADLESS_SIZE` (default 1920x1080); without
  `BASALT_HEADLESS_GPU=1` it uses `WLR_RENDERER=pixman` and Qt's software
  renderer (`QT_QUICK_BACKEND=software`), so no GPU or Mesa driver is
  needed;
- uses its own socket and state, so it can run next to a desktop session
  of the same user: `$XDG_RUNTIME_DIR/basalt-shell-headless/shell.sock`
  and `~/.local/state/basalt-shell-headless/`;
- turns animations off (weak hardware) and runs no idle lock;
- starts `wayvnc` on `BASALT_HEADLESS_VNC` (default `127.0.0.1:5910`,
  localhost only) for the person who confirms;
- has the daemon hold a virtual keyboard and pointer for the whole session
  (`zwp_virtual_keyboard_v1`, `zwlr_virtual_pointer_v1`): with no input
  device the seat would have no keyboard or pointer at all and clients
  would never receive input.

Change the size or the VNC address with `systemctl --user edit
basalt-headless`:

```ini
[Service]
Environment=BASALT_HEADLESS_SIZE=1600x1000
Environment=BASALT_HEADLESS_VNC=127.0.0.1:5911
```

## Connect the agent

The agent's MCP client starts the shell's MCP server with the headless
socket:

```json
{ "mcpServers": { "basalt-desktop": {
    "command": "basalt-shell", "args": ["mcp"],
    "env": { "BASALT_SHELL_SOCKET": "/run/user/1000/basalt-shell-headless/shell.sock" } } } }
```

(1000 is the user's uid.) With the Basalt SELinux policy `basalt-shell
mcp` runs confined in `basalt_agent_mcp_t` (docs/selinux.md).

## Confirm as the person

Every change the agent asks for, every screenshot outside a control
session and every control session waits on the confirmation sheet of the
headless screen. Watch and answer it through VNC, over SSH:

```sh
ssh -L 5910:127.0.0.1:5910 USER@HOST
# then a VNC viewer on localhost:5910
```

Nobody watching means nothing is confirmed: requests expire after 5
minutes and the agent is told so.

## What the agent can do there

| Tool | Notes |
|---|---|
| `desktop_state`, `toplevels_list`, `theme_get`, `apps_list`, `activity_recent` | read at once |
| the typed write tools (`app_launch`, `window_*`, `windows_arrange`, `theme_*` and the rest) | each waits for the person |
| `screen_capture` | a PNG of the screen or of a window, confirmed per screenshot |
| `agent_control_request` | a 1 to 15 minute session: screenshots without asking and `input_*` (type, keys, pointer, scroll); a frame and a banner show it; Super+Shift+Escape or Stop ends it |

## Checked in the lab (2026-10-04)

- the desktop VM with its 3D device: `basalt-headless.service` next to the
  desktop session (pixman, Qt software rendering);
- the same VM started with a 2D display only (`lab/run-gl-vm.sh start
  nogpu`: llvmpipe for the desktop session) and with no display device at
  all (`start server`), the headless session alone;
- in each: the shell UI renders, `lab/demo/agent-demo.py` (a scripted MCP
  client standing in for a model) lists windows, gets a confirmed window
  screenshot, is refused input without a session, gets a control session,
  clicks into a terminal, types a command and Enter, takes a screenshot
  inside the session, is refused input while another request waits for
  the person, ends its session and is refused input again. Screenshots in
  the report.
