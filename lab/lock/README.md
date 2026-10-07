# Lab: the lock screen

The shell's lock screen, headless, in a container: no VM, no GPU, no
network. Nothing here is installed by a package.

| File | What |
|---|---|
| `run.sh` | on the host: starts the dev container (Fedora's sway, swaylock, Quickshell, PAM, wtype, grim), runs the three scripts below, removes the container |
| `prep.sh` | in the container, as root: the lab person's password (`basalt-lab-pass`) and the `basalt-lock` PAM service |
| `../keyboard/session.sh` | in the container: builds and installs this checkout, starts `basalt-session headless` |
| `drive.sh` | in the container: a second output, `basalt-lock`, the keys, a check after each step and the screenshots |

```sh
lab/lock/run.sh build/lock
LANG=pt_BR.UTF-8 lab/lock/run.sh build/lock-pt
```

What it checks: the lock is confirmed by the compositor before
`basalt-lock` returns, the password field has the keyboard at once (on
the output that had the focus; keys typed on the other output reach it),
`ipc call lock unlock` does not exist, a wrong password keeps the session
locked (real PAM: pam_unix through unix_chkpwd), Escape, Caps Lock
(guessed from the typed letters: the host's LEDs are hidden), the hint
after three wrong passwords, the right password unlocks; the shell UI
killed while locked (swaylock takes the lock over, the right password
unlocks it, the shell UI comes back); `basalt-lock` without a shell UI
(the swaylock fallback). In the container swaylock cannot load the
wallpaper (its image loader's sandbox needs namespaces), so it shows the
theme's color there.

What needs a VM (logind, idle, sleep, SELinux, niri, the keyboard LED):
lock with `loginctl lock-session`, an idle lock through swayidle, the lock
before suspend (`journalctl -t basalt-lock` shows it before the kernel's
"suspend entry"), Caps Lock from the LED, the same steps on niri, and
`ausearch -m AVC` empty. The lab VMs of `lab/updates/` (an overlay on an
installed desktop disk) serve for that, with the RPMs of this checkout
(`scripts/build-rpm.sh`) installed over the published ones.
