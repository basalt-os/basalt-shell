#!/bin/bash
# Zero-setup lab, step 2 (as root in the VM): a fresh person.
#   vm-user.sh NAME LANG     e.g. ana pt_BR.UTF-8
# The password is the first 12 characters of /root/desk-user.pass.
# Not an administrator (no wheel). The login screen starts their Basalt
# session on sway at boot (greetd's initial session), as after an install.
# Lab-only addition: a microphone fed from a pipe (the VM has no sound
# card input), the same as lab/voice/vm-lab-setup.sh.
set -euo pipefail
name=$1 lang=$2
id "$name" >/dev/null 2>&1 || useradd -m -c "Lab person" "$name"
echo "$name:$(head -c 12 /root/desk-user.pass)" | chpasswd
printf "LANG=%s\n" "$lang" >/etc/locale.conf
cp -n /etc/greetd/config.toml /root/greetd.config.toml.before-zs || true
cat >/etc/greetd/config.toml <<TOML
[terminal]
vt = 1

[default_session]
command = "tuigreet --time --remember --remember-session --sessions /usr/share/wayland-sessions --cmd 'basalt-session sway'"
user = "greetd"

[initial_session]
command = "basalt-session sway"
user = "$name"
TOML
home=$(getent passwd "$name" | cut -d: -f6)
install -d -o "$name" -g "$name" "$home/.config/systemd/user/default.target.wants"
cat >"$home/.config/systemd/user/lab-mic.service" <<'UNIT'
[Unit]
Description=Lab microphone (PipeWire pipe source; lab only)
After=pipewire-pulse.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c "sleep 2; rm -f %t/lab-mic.fifo; pactl load-module module-pipe-source source_name=lab_mic file=%t/lab-mic.fifo format=s16le rate=16000 channels=1 source_properties=device.description=Lab-microphone ; for i in 1 2 3 4 5 6 7 8 9 10; do pactl set-default-source lab_mic && break; sleep 1; done"
ExecStop=/bin/sh -c "pactl unload-module module-pipe-source"
[Install]
WantedBy=default.target
UNIT
ln -sf ../lab-mic.service "$home/.config/systemd/user/default.target.wants/lab-mic.service"
chown -R "$name:$name" "$home/.config"
restorecon -RF "$home" >/dev/null 2>&1 || true
systemctl restart greetd
