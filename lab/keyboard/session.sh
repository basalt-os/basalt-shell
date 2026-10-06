#!/bin/bash
# Inside the dev container (scripts/dev/Containerfile): build and install
# this checkout (mounted read only at /src), then start a headless Basalt
# session (sway headless, pixman, Qt software rendering) and wait until the
# shell UI answers on its IPC. Run by lab/keyboard/run.sh.
set -euo pipefail
export XDG_RUNTIME_DIR=/tmp/xdg
mkdir -p "$XDG_RUNTIME_DIR" && chmod 700 "$XDG_RUNTIME_DIR"
rm -rf /tmp/src /tmp/stage
cp -r /src /tmp/src
make -s -C /tmp/src build >/dev/null
make -s -C /tmp/src install DESTDIR=/tmp/stage PREFIX=/usr SYSCONFDIR=/etc >/dev/null
export PATH=/tmp/stage/usr/bin:$PATH BASALT_SHELL_DATA=/tmp/stage/usr/share/basalt-shell
export BASALT_HEADLESS_SIZE=${BASALT_HEADLESS_SIZE:-1600x1000}
# A fixed language for the captures.
export LANG=${LANG:-en_US.UTF-8}
nohup dbus-run-session -- basalt-session headless >/tmp/session.log 2>&1 &
for _ in $(seq 1 120); do
  if [ -S "$XDG_RUNTIME_DIR/wayland-1" ] && WAYLAND_DISPLAY=wayland-1 basalt-shell-ui ipc call shell surfaces >/dev/null 2>&1; then
    echo "session up"
    exit 0
  fi
  sleep 0.5
done
echo "session did not come up" >&2
tail -50 /tmp/session.log >&2
exit 1
