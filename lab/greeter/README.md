# Lab: the login screen and the boot splash

Tools used to test basalt-greeter and the Basalt boot splash
(plymouth-theme-basalt, from the Basalt OS repository) on a real Basalt OS
desktop install. Nothing here is installed by a package.

| File | What |
|---|---|
| `run-greeter-vm.sh` | the VM: a qcow2 overlay on an installed desktop disk (the backing file is never written), a second 1 GB disk for a LUKS data volume, Secure Boot firmware, no TPM device; `start gl` (virtio-gpu with virgl) or `start nogpu` (llvmpipe) |
| `dev-install.sh` | install the greeter from a source tree in the VM, at the package's paths (development only) |
| `dev-selinux.sh` | build and load `basalt_greeter` in the VM; `PERMISSIVE=1` to collect denials while writing the policy |
| `vm-type.py` | type through the QEMU monitor (`sendkey`): VNC typing makes QEMU toggle Caps Lock to match each letter's case, which a login test must not have |
| `fake-greetd/` | greetd stand-in with greetd 0.10's answers (`internal/greetd`), no PAM |
| `e2e-fake.sh` | the real UI in a headless sway against the fake greetd: wrong password, right password, each person's session, status, no password in any log |

## The run (2026-10-06, basalt-shell 0.7.0, plymouth-theme-basalt 0.3.0)

On a Basalt OS desktop VM (SELinux enforcing) with the RPMs from this
tree and from the Basalt OS repository's `greeter` branch:

1. Two people (`basalt`, `ana`); a LUKS2 data volume in `/etc/crypttab`
   without a key file, so every boot asks its passphrase; the kernel
   command line of the desktop edition (`rhgb quiet
   plymouth.ignore-serial-consoles`).
2. Restart from the greeter's power menu: the shutdown splash, the GRUB
   menu, the boot splash, the disk card; a wrong passphrase ("That did not
   work"), then the right one; the greeter.
3. Wrong password, Caps Lock, the session menu (Basalt on niri for one
   person, Basalt on Sway for the other, each remembered), power,
   accessibility (large text, high contrast), Brazilian Portuguese, "Other
   user", the fade into the session, both people logged in.
4. Fallback: the greeter UI killed, and a broken QML file: greetd's
   greeter command starts tuigreet on the same terminal; logging in there
   works.
5. The same boot and greeter without a GPU (llvmpipe).
6. Denials: none for `basalt_greeter_t` with the dontaudit rules on; with
   them off, only the deliberate ones (Qt's JIT probe, Quickshell's file
   watch). No password in the journal, the greeter's logs or its state.

A selection of the run's screenshots
is in `media/greeter/` and [docs/greeter.md](../../docs/greeter.md).

## Several monitors (2026-10-08, basalt-shell 0.9.4)

The login card, the people and the top bar's controls follow the pointer:
they are on the output the person last moved the pointer onto (the first
output until then), and the keyboard goes with them. The headless e2e
cannot check it (a headless sway has no pointer device, so nothing hovers);
on a lab VM with the real greeter:

1. Add a second output to the greeter's sway, as root:
   `S=$(ls /run/basalt-greeter/xdg/sway-ipc.*.sock); runuser -u greetd -- env SWAYSOCK=$S swaymsg create_output`.
2. Move the pointer with the HID tablet through QMP (`mouse_set` to the
   tablet in the monitor, then `input-send-event` with absolute x and y
   from 0 to 32767 over the whole layout) onto each output.
3. Capture each output: `runuser -u greetd -- env XDG_RUNTIME_DIR=/run/basalt-greeter/xdg WAYLAND_DISPLAY=wayland-1 grim -o NAME FILE`.
4. Type the password (`vm-type.py --enter`): the session starts from the
   card on either output.
