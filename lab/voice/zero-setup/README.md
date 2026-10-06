# Zero-setup voice test

What it checks, on a desktop lab VM (QEMU with virgl, no TPM device),
with a person who never logged in before and is not an administrator:
the voice service runs at login without any command (user preset), no
password prompt appears, push to talk offers the speech model for the
session's language (Download, Not now), the download (polkit, the
confined basalt-models service) shows its progress and a notification,
push to talk then works (a command and a dictation), the same with the
network down (the card waits, the download starts again by itself), and
Settings, Voice and assistant, downloads the assistant's local model,
after which the command bar uses it.

1. Build the RPMs (basalt-os: `packages/basalt-shell/build.sh` with
   `BASALT_SHELL_SRC`, `packages/basalt-voice/build.sh`,
   `packages/basalt-llm/build.sh`, `scripts/build-rpms.sh`) and copy the
   binary RPMs into the VM.
2. `vm-prep.sh RPM_DIR` in the VM, then `vm-user.sh NAME pt_BR.UTF-8`
   (or `en_US.UTF-8`); greetd logs the person in after its next start
   (remove `/run/greetd.run` and restart greetd, or reboot).
3. From the host, as a person would: Super+V through the QEMU monitor
   (`sendkey meta_l-v 300`), clicks through VNC on the card's Download
   button, `set_link <netdev> off` and `on` for the offline case (it
   also cuts SSH through user networking: watch the screen), speech with
   `feed.sh` while holding the key (`sendkey meta_l-v 6000`).
4. Check: `/run/basalt-models/*.state`, the person's activity log
   (`~/.local/state/basalt-shell/audit.jsonl`), `basalt-ledger --all
   --producer basalt-models`, `ausearch -m avc -ts boot`.

The speech clips come from Piper voices kept in the lab only (an English
public-domain voice; for Portuguese a voice that is not allowed in the
product, used only to make test audio).
