#!/bin/bash
# Try the Basalt shell without installing anything on your system: a
# Fedora 44 container with sway, niri and Quickshell runs a nested
# session in a window of your current Wayland desktop. Its home, settings
# and notifications stay inside the container.
#
#   scripts/try-podman.sh [sway|niri]
#
# Needs: podman, a Wayland session, a Mesa GPU (Intel, AMD; NVIDIA with the
# proprietary driver falls back to software rendering, slower).
set -euo pipefail
cd "$(dirname "$0")/.."
which=${1:-sway}
img=localhost/basalt-shell-dev:44
[ -n "${WAYLAND_DISPLAY:-}" ] || { echo "run this from a Wayland session" >&2; exit 1; }
if ! podman image exists "$img"; then
  echo "building $img (once, a few minutes)"
  podman build -t "$img" -f scripts/dev/Containerfile scripts/dev
fi
host_sock="${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY}"
gpu=()
[ -d /dev/dri ] && gpu=(--device /dev/dri)
exec podman run --rm -it --userns=keep-id --cap-add=SYS_NICE --security-opt label=disable \
  "${gpu[@]}" --shm-size=512m \
  -v "$PWD:/src:ro" -v "$host_sock:/run/host-wayland:rw" \
  -e WAYLAND_DISPLAY=/run/host-wayland -e XDG_RUNTIME_DIR=/tmp/xdg \
  -e BASALT_TRY="$which" \
  "$img" bash -euc '
    mkdir -p /tmp/xdg && chmod 700 /tmp/xdg
    cp -r /src /tmp/src && make -s -C /tmp/src install DESTDIR=/tmp/stage PREFIX=/usr SYSCONFDIR=/etc >/dev/null
    export PATH=/tmp/stage/usr/bin:$PATH BASALT_SHELL_DATA=/tmp/stage/usr/share/basalt-shell
    export BASALT_SHELL_APPS=1   # the container has its own settings, so let apps follow the theme
    exec dbus-run-session -- basalt-session "$BASALT_TRY"
  '
