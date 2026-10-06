# Confirmation boundary: SELinux domains and peer checks

Agents may ask the desktop for anything in the closed set of typed
actions; only the person confirms. This page describes how that is
enforced, what was tested and what it does not cover.

## Domains

| Domain | Entered by | Role |
|---|---|---|
| `basalt_shell_t` | executing `/usr/bin/basalt-shelld` (`basalt_shell_exec_t`) | the daemon: owns desktop state and every change, checks each peer of its socket |
| `basalt_shell_ui_t` | executing `/usr/libexec/basalt-shell/basalt-shell-ui-launch` (`basalt_shell_ui_exec_t`) | the shell UI (Quickshell). The only domain whose connections may confirm |
| `basalt_agent_mcp_t` | executing `/usr/bin/basalt-shell` (`basalt_agent_mcp_exec_t`) | the shell's MCP server and command line: an agent domain |

Files: the daemon's socket directory `$XDG_RUNTIME_DIR/basalt-shell` (and
`basalt-shell-headless`) is `basalt_shell_runtime_t`, created by the
daemon through a named type transition.

The session (greetd, `unconfined_t` on Fedora) starts the three programs
and the kernel moves each into its domain. The UI launcher pins what runs
in the UI domain: Quickshell from `/usr/bin`, the QML installed next to
the launcher, and an environment without plugin, import or preload paths
(`QT_PLUGIN_PATH`, `QML2_IMPORT_PATH`, `LD_*`, `BASALT_SHELL_QML` and the like).

## The check on the socket

Every connection is classified by what the kernel says about the peer,
not by what the client claims:

- `SO_PEERCRED`: same uid as the daemon, else the connection is closed;
- `SO_PEERSEC`: the peer's SELinux context; the `ui` role (confirm,
  decline, answer a choice, act directly) is granted only when its type is
  `basalt_shell_ui_t` and its executable is Quickshell;
- one UI connection at a time: a second process asking for the `ui` role
  while the shell UI is connected is refused (the same process
  reconnecting replaces its old connection).

Every other connection is an agent: it can read, propose and wait. Each
refusal is written to the activity log with the peer's pid, program and
context. The daemon picks the mode at start (`BASALT_SHELL_UI_CHECK`):

| Mode | When | Who may confirm |
|---|---|---|
| `selinux` | the `basalt_shell` policy is loaded (default when it is) | peers in `basalt_shell_ui_t` running Quickshell |
| `exe` | no policy (containers, systems without SELinux) | any process named `quickshell` of the user: weak, shown as such in Settings |
| `insecure` | development and unit tests only | any process of the user |
| `none` | `selinux` required but the policy missing | nobody |

The audit actor carries the domain from the kernel, for example
`agent:mcp (basalt_agent_mcp_t)`; the client name is only a label.

## What the agent domain may do

`basalt_agent_mcp_t` may connect to the daemon's socket and nothing else
of the desktop: no Wayland socket (so no input injection, screen capture
or clipboard of its own), no X11 socket, no D-Bus session bus, no
PipeWire, no files in the home directory, no network, no transition into
the UI or daemon domains. Screenshots and input exist only through the
daemon, which applies the confirmation, control-session and audit rules
(design.md, "Agents' last resort"). It writes nothing on disk:
`basalt-shell screenshot` writes the PNG to stdout, MCP clients get it
inline.

## Applications the shell starts

An application started from the launcher, the command bar or an agent's
confirmed `app.launch` runs in the person's own domain, the one their
session runs in (`unconfined_t` for an unconfined user, the login domain
of a confined one), never in `basalt_shell_t`. The daemon does not execute
the program itself: it asks the systemd user manager to start a transient
service (`systemd-run --user`, unit `app-basalt-<id>-<random>.service` in
`app.slice`, `Type=exec`, `ExitType=cgroup`), and the manager, which runs
in the person's domain, starts it. A scope (`systemd-run --scope`) would
not do: the program would be a child of the daemon and inherit
`basalt_shell_t`, which versions up to 0.3.1 did. Without a user manager
(containers, nested test sessions) the compositor starts the program, also
outside the daemon.

Why it matters: the daemon's type is its identity. A program in
`basalt_shell_t` could not confirm (only `basalt_shell_ui_t` can), but it
would carry whatever the daemon's domain is allowed, now or once it is
confined, and its denials and audit records would name the shell. The
session's environment (display, toolkit and compositor variables) is
passed to each service explicitly, so headless sessions work the same.

## The agent family (shared with basalt-agent)

`basalt_agent_mcp_t` is a member of the Basalt agent family. The family's
base module, `basalt_agent_base`, lives with basalt-agent ([basalt-os](https://github.com/basalt-os/basalt-os),
`packages/basalt-agent/selinux`, package `basalt-agent-selinux`): the
attribute `basalt_agent_domain`, the baseline `basalt_agent_domain_type()`,
helpers such as `basalt_agent_use_terminals()`, and the neverallow rules
of every agent (no credentials, no home or user runtime sockets, no
escalation). This module builds on it and adds the shell's part of
"agents request, the person confirms":

| In `basalt_shell` | Use |
|---|---|
| `basalt_shell_request(domain)` (interface) | let an agent domain reach the daemon's socket (search the user runtime directory, write the `basalt_shell_runtime_t` socket, `connectto` `basalt_shell_t`); given to `basalt_agent_mcp_t`, available to basalt-agent's domains |
| `basalt_shell_mcp_run(domain, role)` (interface) | let another agent domain run the shell's MCP server in `basalt_agent_mcp_t` |
| neverallow rules | no `basalt_agent_domain` may transition into, trace, execute the entry point of, or connect to `basalt_shell_ui_t`, or transition into the daemon |

The confirming side is the daemon's check, not a policy attribute: a
daemon that accepts confirmations (the shell today, the system assistant
later) reads `SO_PEERSEC` and accepts only its UI domain, as
`internal/shell/peer.go` does; the policy keeps every agent out of that
domain.

Fedora's policy store does not check `neverallow` at install time
(`expand-check = 0`); the tests below check the same things by behaviour,
and `sesearch` shows no transition, `connectto` or `ptrace` from
`basalt_agent_mcp_t` to the UI or daemon domains besides `connectto` on
the daemon's socket.

(The first version of this work had its own small base module with the
same name; it was replaced by the one from basalt-agent before release.)

## Tests (lab VM, enforcing, `lab/demo/security-test.sh`)

| Test | Result |
|---|---|
| T1 an `unconfined_t` process (python) asks for the ui role and confirms an agent's request | refused: domain `unconfined_t` |
| T2 a copy of python named `quickshell` (passes the old program-name check) | refused: domain `unconfined_t` |
| T3 `basalt-shell ctl` (`basalt_agent_mcp_t`) with the ui role | refused: domain `basalt_agent_mcp_t` |
| T4 the MCP server: no tool can confirm; the request stays pending | pending |
| T5 unconfined code switching itself to `basalt_shell_ui_t` (setcon) | denied by SELinux (`dyntransition`) |
| T5b a second, genuine shell UI started while the first runs | refused: another shell UI is connected |
| T6 to T9 `basalt_agent_mcp_t` connecting to the Wayland, D-Bus, X11 and PipeWire sockets | permission denied (SELinux; the base policy does not audit these) |
| T10 the request after all attempts | still pending |
| T11 the person presses Enter on the confirmation sheet | applied, decided by `ui` |
| T12 an agent's confirmed `app.launch` (foot); the launcher (Firefox) | each in its own `app-basalt-*.service`, running in `unconfined_t`, the session's domain |
| T13 processes in `basalt_shell_t` after the launches | only `basalt-shelld` |

AVC denials during normal use (session start, shell UI, command bar, MCP
tools, screenshots and a control session, the headless session, apps
started from the shell, among them Firefox playing audio with realtime
threads from rtkit): 0. The only denials are those of T5 to T9, the
attacks being refused (depending on the base policy version they are
logged or hidden by `dontaudit` rules, visible with `semodule -DB`).
Retested on the 0.3.1 live desktop image (SwayFX, enforcing) with the
launch change: T1 to T13 pass.

Before that change, an app started from the shell ran in
`basalt_shell_t`. Firefox asks rtkit for a realtime audio thread at
start, and `rtkit_daemon_t` may not set the scheduler of `basalt_shell_t`:
the likely source of the rtkit AVC seen on the first live desktop image,
gone since apps run in the person's domain (rtkit now grants Firefox's
thread, no denial).

## Voice and skill domains (0.4)

Five more domains hold the voice service and the skills' workers. They
form the attribute `basalt_skill_family` and are started by the shell
daemon (the voice service by the user unit):

| Domain | Program | May | May not |
|---|---|---|---|
| `basalt_voice_t` | basalt-voiced | PipeWire (its socket type `basalt_pipewire_sock_t`), its runtime directory, the speech models | network, home files, Wayland, D-Bus |
| `basalt_skill_index_t` | basalt-skill-index | read plain home content (`user_home_t`) of granted folders, write the index | network, keys, configuration |
| `basalt_skill_t` | basalt-skill | read the index; web, IMAP and DNS ports in a per-job network session | home files, credentials, SMTP |
| `basalt_skill_send_t` | basalt-skill-send | SMTP and DNS in a session that allows only the account's server | the index, the mailbox, web ports, home files |
| `basalt_skill_files_t` | basalt-skill-files | list, create and rename in plain home content | open, read, write or delete a file; network |

The person's voice and assistant settings (`~/.config/basalt`, type
`basalt_user_conf_t`, given to the directory when the daemon creates it)
are read and written only by the daemon; none of these domains opens
them (they get the values with each request), and `neverallow` rules
keep every agent domain and every domain of the family from writing
them.

None of them may run a general program. The few tools they need get
types of their own, `basalt_voice_tool_exec_t` (whisper-cli, Piper,
pw-cat), `basalt_pdf_tool_exec_t` (pdftotext, pdfinfo) and
`basalt_browser_exec_t` (Chromium), which every other domain may still run
as before. `neverallow` rules keep `bin_t`, `shell_exec_t`, the setuid
helpers' types (sudo, su, passwd, chfn, mount, fusermount and others;
pkexec and newgrp are `bin_t`), `usr_t`, `etc_t`, running a library or the
dynamic loader as a program, and running anything they can write (home,
temporary and memory files) out of these domains. The workers and the
voice service also set `no_new_privs`. Fedora's `corecmd_exec_bin` is
not used: on Fedora it grants every `base_ro_file_type`, the shell
included. The lab's escape matrix (`lab/voice/tests/escape-test.sh`)
checks each domain against what it may do.

## Limits

- This is a boundary against confined agents. Code running unconfined as
  the same user (`unconfined_t`) is outside SELinux's control: it can run
  the UI launcher, trace processes or send input through the kernel
  (uinput, with access to it). The one-UI rule makes a takeover visible
  (the running shell would have to go away first), it does not prevent
  it. Confined user roles (`staff_u`/`user_u`) and confined agent
  launchers (basalt-agent) are the way to shrink what runs unconfined.
- The daemon and the UI are `unconfined_domain` for now: their types
  identify them, they are not themselves confined.
- An agent CLI that is not confined can bypass the MCP server and talk to
  the socket directly; it is still only an agent there.
