#!/bin/bash
# Lock screen test, headless: the dev container (scripts/dev/Containerfile,
# with Fedora's sway, swaylock and PAM) runs a headless Basalt session with
# this checkout's daemon and QML and two outputs, locks it the way
# Super+L and swayidle do (basalt-lock), types into it with wtype and
# captures each step with grim. The person's password is checked by the
# real PAM stack of the container (basalt-lock service, pam_unix,
# unix_chkpwd).
#
#   lab/lock/run.sh [OUTDIR]               (default: build/lock)
#   LANG=pt_BR.UTF-8 lab/lock/run.sh build/lock-pt
#   IMG=localhost/other:44 lab/lock/run.sh
#
# Needs podman. No network; the container is removed at the end.
set -euo pipefail
cd "$(dirname "$0")/../.."
out=$(readlink -f "${1:-build/lock}")
mkdir -p "$out"
name=basalt-lock-test-$$
img=${IMG:-localhost/basalt-shell-dev:44}
podman image exists "$img" || podman build --network host -t "$img" -f scripts/dev/Containerfile scripts/dev
noleds=$(mktemp -d)
cleanup() { podman rm -f "$name" >/dev/null 2>&1 || true; rmdir "$noleds"; }
trap cleanup EXIT
# The host's keyboard LEDs are hidden (an empty /sys/class/leds): Caps Lock
# is then read from the typed letters, as on a computer without the LED.
podman run -d --rm --init --name "$name" --network none --userns=keep-id --cap-add=SYS_NICE --security-opt label=disable \
  -v "$noleds:/sys/class/leds:ro" \
  --shm-size=512m -e LANG="${LANG:-en_US.UTF-8}" -v "$PWD:/src:ro" -v "$out:/out:rw" "$img" sleep infinity >/dev/null
# As root in the container: the lab person's password and the PAM service.
podman exec -u root "$name" bash /src/lab/lock/prep.sh "$(podman exec "$name" id -un)"
podman exec "$name" bash /src/lab/keyboard/session.sh
podman exec "$name" bash /src/lab/lock/drive.sh
