#!/bin/bash
# Voice spike demo (lab VM, session user, inside the running sway
# session): spoken requests through the lab microphone (lab-say holds
# Super+V), permissions confirmed by clicking Allow, the answers spoken.
# Screenshots (grim) and WebM recordings (wf-recorder, with the speakers'
# audio) go to OUTDIR.
#   voice-demo.sh OUTDIR [scene ...]   scenes: files mail web permissions
set -u
out=${1:?output directory}; shift
mkdir -p "$out"
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
export WAYLAND_DISPLAY=$(ls "$XDG_RUNTIME_DIR" | grep -E '^wayland-[0-9]+$' | head -1)
export SWAYSOCK=$(ls -t "$XDG_RUNTIME_DIR"/sway-ipc.*.sock | head -1)
export YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/run/ydotoold.socket}
export LAB_SAY_ECHO=1
log=$HOME/.local/state/basalt-shell/audit.jsonl
monitor=$(pactl get-default-sink).monitor
shot() { grim "$out/$1.png"; echo "shot $1"; }
rec_start() { wf-recorder -y -a="$monitor" -c libvpx -p deadline=good -p cpu-used=4 -p b=4M -C libopus -f "$out/$1.webm" >/dev/null 2>&1 & REC=$!; sleep 1; }
rec_stop() { sleep 1; kill -INT "$REC" 2>/dev/null; for _ in $(seq 30); do kill -0 "$REC" 2>/dev/null || break; sleep 0.5; done; wait "$REC" 2>/dev/null; echo "rec $1"; }
ui() { basalt-shell-ui ipc call shell "$@" >/dev/null; }
# Wait until the voice turn ends (the "voice" record with timing).
wait_turn() {
  local before=$1
  for _ in $(seq 600); do
    tail -n +$((before + 1)) "$log" | grep -q '"type":"voice","actor":"voice"' && return 0
    sleep 0.5
  done
}
wait_skill() {
  local before=$1
  for _ in $(seq 600); do
    tail -n +$((before + 1)) "$log" | grep '"type":"skill","actor":"skill"' | grep -q '"answer"' && return 0
    sleep 0.5
  done
}
click() { ydotool mousemove -a -x "$1" -y "$2" >/dev/null; sleep 0.3; ydotool click 0xC0 >/dev/null; }
# Allow: the first button under the permission in the command bar. Its
# position depends on the text; the demo looks for it with a fixed probe
# list (lab screen 1920x1080) unless ALLOW_X/ALLOW_Y are set.
allow() {
  local n; n=$(grep -c . "$log")
  for y in 321 340 360 380; do
    click 636 "$y"; sleep 1.5
    tail -n +$((n + 1)) "$log" | grep -q '"text":"grant:' && return 0
  done
}

say() { # say TEXT: a spoken request, then wait for the whole turn
  local n; n=$(wc -l <"$log")
  ( sleep 1.4; shot "$2-listening" ) &
  lab-say --text "$1"
  wait_turn "$n"
}

scene_files() {
  ui close; sleep 0.5
  rec_start files
  say "Find the PDF the bank sent last month." files-1
  sleep 2; shot files-2-permission
  local n; n=$(wc -l <"$log")
  allow
  wait_skill "$n"; sleep 6
  shot files-3-answer
  rec_stop files
}
scene_mail() {
  ui close; sleep 0.5
  rec_start mail
  say "Summarize the email about the invoice that is overdue." mail-1
  sleep 2; shot mail-2-permission
  local n; n=$(wc -l <"$log")
  allow
  wait_skill "$n"; sleep 8
  shot mail-3-answer
  rec_stop mail
}
scene_web() {
  ui close; sleep 0.5
  rec_start web
  ui ask "summarize http://news.lab.test/w04-js-exfil.html"
  sleep 3; shot web-1-permission
  local n; n=$(wc -l <"$log")
  allow
  wait_skill "$n"; sleep 3
  shot web-2-answer
  rec_stop web
}
scene_permissions() {
  ui close; sleep 0.5; ui open commandbar; sleep 1.5; shot permissions
  ui close
}
scene_reset() { basalt-shell-ui ipc call shell close >/dev/null; sleep 1; ui ask "revoke access"; sleep 2; ui close; sleep 1; }
scenes=${*:-reset files mail web permissions}
for s in $scenes; do "scene_$s"; done
