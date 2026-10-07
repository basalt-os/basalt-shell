#!/bin/bash
# Keyboard navigation test of the shell, headless: a dev container
# (scripts/dev/Containerfile) runs sway headless with this checkout's
# daemon and QML, wtype types the keys a person would (through the
# compositor, as from a keyboard), the shell's IPC says which control
# holds the keyboard after each step, and grim captures the focus rings;
# then Settings, Keyboard (settings.sh).
#
#   lab/keyboard/run.sh [OUTDIR]       (default: build/keyboard)
#   IMG=localhost/other:44 lab/keyboard/run.sh
#
# Needs podman and an image with Fedora's sway (pixman renderer): an image
# with SwayFX instead draws with GLES only and needs a GPU. No network,
# nothing installed on the host; the container is removed at the end.
set -euo pipefail
cd "$(dirname "$0")/../.."
out=$(readlink -f "${1:-build/keyboard}")
mkdir -p "$out"
name=basalt-kbd-test-$$
img=${IMG:-localhost/basalt-shell-dev:44}
podman image exists "$img" || podman build -t "$img" -f scripts/dev/Containerfile scripts/dev
cleanup() { podman rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT
podman run -d --rm --name "$name" --network none --userns=keep-id --cap-add=SYS_NICE --security-opt label=disable \
  --shm-size=512m -v "$PWD:/src:ro" -v "$out:/out:rw" "$img" sleep infinity >/dev/null
podman exec "$name" bash /src/lab/keyboard/session.sh
podman exec "$name" bash /src/lab/keyboard/drive.sh
podman exec "$name" bash /src/lab/keyboard/settings.sh
