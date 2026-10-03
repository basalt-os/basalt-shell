#!/bin/bash
# Run an installed Basalt shell (scripts/install.sh) nested in a window of
# your current desktop, on its own D-Bus session bus, without touching the
# host session's settings.
#   scripts/try-nested.sh [sway|niri]
set -euo pipefail
which=${1:-sway}
command -v basalt-session >/dev/null || { echo "basalt-shell is not installed (scripts/install.sh)" >&2; exit 1; }
export BASALT_SHELL_SOCKET="${XDG_RUNTIME_DIR}/basalt-shell-nested/shell.sock"
# Separate config and state so the nested shell does not change yours.
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}/basalt-shell-nested"
export XDG_STATE_HOME="${XDG_STATE_HOME:-$HOME/.local/state}/basalt-shell-nested"
mkdir -p "$XDG_CONFIG_HOME" "$XDG_STATE_HOME"
exec dbus-run-session -- basalt-session "$which"
