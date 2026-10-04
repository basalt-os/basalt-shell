#!/bin/bash
# Start (or stop) a second basalt-shelld for the automated skill tests, in
# the person's graphical session (so the model helper's polkit rule
# applies), on its own socket and audit log, without a compositor, with
# the UI check off so skillctl.py can confirm like the shell UI. The
# skills, workers, sessions, resolver and model are the real ones.
#   test-daemon.sh start|stop     (as the session user)
set -eu
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}
sock=$XDG_RUNTIME_DIR/basalt-shell-test/shell.sock
case "${1:-start}" in
  start)
    export SWAYSOCK=$(ls -t $XDG_RUNTIME_DIR/sway-ipc.*.sock | head -1)
    swaymsg -q exec "env -u SWAYSOCK -u WAYLAND_DISPLAY -u NIRI_SOCKET BASALT_SHELL_SOCKET=$sock BASALT_SHELL_UI_CHECK=insecure BASALT_SHELL_NO_APPS=1 BASALT_SHELL_VOICE=0 BASALT_SHELL_VIRTUAL_INPUT=0 XDG_STATE_HOME=$HOME/.local/state/skilltest basalt-shelld >$XDG_RUNTIME_DIR/skilltest.log 2>&1"
    for _ in $(seq 40); do [ -S "$sock" ] && break; sleep 0.25; done
    [ -S "$sock" ] && echo "test daemon on $sock" || { cat $XDG_RUNTIME_DIR/skilltest.log; exit 1; }
    ;;
  stop)
    # Only the daemon whose environment names the test socket.
    for p in $(pgrep -u "$(id -u)" -x basalt-shelld); do
      tr '\0' '\n' < /proc/$p/environ 2>/dev/null | grep -qx "BASALT_SHELL_SOCKET=$sock" && kill "$p"
    done; true ;;
esac
