#!/bin/bash
# The checks are code run later by eval: single quotes on purpose.
# shellcheck disable=SC2016
# Inside the lock test container, after prep.sh and lab/keyboard/session.sh:
# lock the session with basalt-lock, type into the lock screen with wtype
# (through the compositor, as from a keyboard) and capture every step with
# grim, on two outputs. Then the shell UI is killed while locked (swaylock
# must take the lock over) and basalt-lock runs without a shell UI (the
# swaylock fallback). Exit status 1 when a check fails. Run by
# lab/lock/run.sh.
set -uo pipefail
export XDG_RUNTIME_DIR=/tmp/xdg WAYLAND_DISPLAY=wayland-1 PATH=/tmp/stage/usr/bin:$PATH
export BASALT_SHELL_SOCKET=$XDG_RUNTIME_DIR/basalt-shell-headless/shell.sock
export BASALT_SHELL_DATA=/tmp/stage/usr/share/basalt-shell
SWAYSOCK=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'sway-ipc.*.sock' 2>/dev/null | head -1)
export SWAYSOCK
pass=0
fail=0
good=basalt-lab-pass

swaylock_running() { pgrep -x swaylock -r R,S,D >/dev/null; }
lock_state() { timeout 5 basalt-shell-ui ipc call lock state 2>/dev/null; }
focused() { timeout 5 basalt-shell-ui ipc call shell focused 2>/dev/null; }
# A keyboard that stays for the whole run (see lab/keyboard/drive.sh).
wtype -k XF86WakeUp -s 900000 -k XF86WakeUp &
keeper=$!
trap 'kill $keeper 2>/dev/null' EXIT
sleep 1
k() { local key; for key in "$@"; do wtype -k "$key"; sleep 0.3; done; }
type_text() { wtype "$1"; sleep 0.3; }
# shot NAME: both outputs, one file each.
shot() { sleep "${2:-0.4}"; grim -o HEADLESS-1 "/out/$1.png"; grim -o HEADLESS-2 "/out/$1-output2.png" 2>/dev/null; }
check() {
  if eval "$1"; then pass=$((pass + 1)); echo "ok    $2"; else fail=$((fail + 1)); echo "FAIL  $2"; fi
}
wait_for() { # wait_for CMD SECONDS
  local _
  for _ in $(seq 1 $(($2 * 10))); do eval "$1" && return 0; sleep 0.1; done
  return 1
}

echo "== a second output"
swaymsg create_output >/dev/null
swaymsg output HEADLESS-2 resolution 1280x800 position 1600 0 >/dev/null
sleep 1
check '[ "$(lock_state)" = unlocked ]' "starts unlocked"

echo "== lock (basalt-lock, as Super+L and swayidle run it)"
t0=$(date +%s%N)
basalt-lock
t1=$(date +%s%N)
echo "      basalt-lock returned after $(((t1 - t0) / 1000000)) ms"
# At once: the clock and the field are there before any key.
grim -o HEADLESS-1 /out/01-locked-at-once.png
check '[ "$(lock_state)" = locked ]' "locked, confirmed by the compositor"
check 'pgrep -f "basalt-lock --guard" >/dev/null' "the guard watches the shell UI"
shot 02-locked 1
# The compositor gives the keyboard to one lock surface: the card's field,
# or the hidden field of another output that types into it.
check '[[ "$(focused)" == lock-password || "$(focused)" == lock-relay ]]' "the password field has the keyboard"

echo "== there is no way to unlock through IPC"
out=$(timeout 5 basalt-shell-ui ipc call lock unlock 2>&1)
echo "      ipc call lock unlock: $out"
check '[[ $out == *"Function not found"* ]]' "ipc call lock unlock does not exist"
timeout 5 basalt-shell-ui ipc show 2>/dev/null | sed -n '/target lock/,/target /p' | head -6
check '[ "$(lock_state)" = locked ]' "still locked"
check '! basalt-shell ctl session.unlock >/dev/null 2>&1' "the daemon has no unlock operation"

echo "== a wrong password"
type_text "not-the-password"
shot 03-typed
k Return
shot 04-checking 0.3
sleep 2.5
shot 05-wrong-password
check '[ "$(lock_state)" = locked ]' "a wrong password keeps it locked"
check '[[ "$(focused)" == lock-password || "$(focused)" == lock-relay ]]' "the field has the keyboard again"

echo "== Escape clears the field"
type_text "abc"
k Escape
shot 06-escape-cleared

echo "== Caps Lock"
k Caps_Lock
type_text "A"
shot 07-caps-lock
k BackSpace Caps_Lock
type_text "a"
k BackSpace

echo "== three wrong passwords: the hint about Caps Lock and the layout"
for p in wrong1 wrong2; do type_text "$p"; k Return; sleep 3; done
shot 08-third-wrong
check '[ "$(lock_state)" = locked ]' "still locked after three wrong passwords"

echo "== the right password"
type_text "$good"
k Return
check 'wait_for "[ \"\$(lock_state)\" = unlocked ]" 8' "the right password unlocks"
shot 09-unlocked 0.8

echo "== the shell UI stops while locked: swaylock takes over"
basalt-lock
check '[ "$(lock_state)" = locked ]' "locked again"
pkill -9 -x quickshell; pkill -9 -x qs
check 'wait_for "swaylock_running >/dev/null" 6' "swaylock took the abandoned lock"
shot 10-crash-swaylock 1.5
type_text "wrong"; k Return; sleep 2.5
check 'swaylock_running >/dev/null' "a wrong password keeps swaylock locked"
type_text "$good"; k Return
check 'wait_for "! swaylock_running >/dev/null" 8' "the right password unlocks swaylock"
check 'wait_for "[ \"\$(lock_state)\" = unlocked ]" 20' "the shell UI is back"
shot 11-after-crash-unlock 2

echo "== no shell UI: basalt-lock falls back to swaylock"
pkill -x quickshell; pkill -x qs
sleep 1
basalt-lock
check 'swaylock_running >/dev/null' "swaylock locked"
shot 12-fallback-swaylock 1
type_text "$good"; k Return
check 'wait_for "! swaylock_running >/dev/null" 8' "the right password unlocks"

echo
echo "passed $pass, failed $fail"
[ "$fail" -eq 0 ]
