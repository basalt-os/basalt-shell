# Lab: the keyboard

The shell driven with the keyboard alone, headless, in a container: no
VM, no GPU, no network. Nothing here is installed by a package.

| File | What |
|---|---|
| `run.sh` | on the host: starts the dev container (Fedora's sway, Quickshell, wtype, grim), runs the two scripts below, removes the container |
| `session.sh` | in the container: builds and installs this checkout, starts `basalt-session headless`, waits for the shell's IPC |
| `drive.sh` | in the container: the keys, a check after each step (`ipc call shell focused`, `surfaces`) and the screenshots |

```sh
lab/keyboard/run.sh build/keyboard
```

What it covers, and the rules it checks: [docs/design.md, Keyboard and
focus](../../docs/design.md#keyboard-and-focus). The login screen has its
own run in `lab/greeter/e2e-fake.sh` (scenario 3, the keyboard alone).
