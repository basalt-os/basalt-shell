# Lab: the keyboard

The shell driven with the keyboard alone, headless, in a container: no
VM, no GPU, no network. Nothing here is installed by a package.

| File | What |
|---|---|
| `run.sh` | on the host: starts the dev container (Fedora's sway, Quickshell, wtype, grim), runs the two scripts below, removes the container |
| `session.sh` | in the container: builds and installs this checkout, starts `basalt-session headless`, waits for the shell's IPC |
| `drive.sh` | in the container: the keys, a check after each step (`ipc call shell focused`, `surfaces`) and the screenshots |
| `settings.sh` | in the container, after `drive.sh`: Settings, Keyboard with the keys alone (the picker, a layout added, moved and removed, the switch key, Caps Lock, the try field), a new session that must bring the settings back, and the system keyboard's sheet (on Not now) with a stand-in for the assistant's `basalt` |

```sh
lab/keyboard/run.sh build/keyboard
```

What it covers, and the rules it checks: [docs/design.md, Keyboard and
focus](../../docs/design.md#keyboard-and-focus). The login screen has its
own run in `lab/greeter/e2e-fake.sh` (scenario 3, the keyboard alone).

A headless sway has no real keyboard, and virtual keyboards (wtype, an
agent's) keep the keymap their client gives them, so `settings.sh` checks
what the shell wrote and what sway accepted, not the characters a layout
produces. That needs a real keyboard: a lab VM, where QEMU's `sendkey`
presses physical keys (`lab/updates/vm-type.py --key semicolon` is the ç
key of an ABNT2 keyboard), as the keyboard settings were checked for 0.9.3.
