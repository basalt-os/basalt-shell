#!/bin/bash
# Demo recordings of 0.4 (lab VM, session user, inside the running sway
# session, after demo-home.sh on and a session restart): spoken requests
# through the lab microphone (lab-say holds Super+V), the person's clicks
# (Allow, Send, Insert) with ydotool where a person would click, the
# answers spoken. 1920x1080 WebM recordings (wf-recorder, with the
# speakers' audio) and PNG screenshots go to OUTDIR.
#   acting-demo.sh OUTDIR [scene ...]
#   scenes: files mail reply invoice timeline dictation
set -u
out=${1:?output directory}; shift
mkdir -p "$out"
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
export WAYLAND_DISPLAY=$(ls "$XDG_RUNTIME_DIR" | grep -E '^wayland-[0-9]+$' | head -1)
export SWAYSOCK=$(ls -t "$XDG_RUNTIME_DIR"/sway-ipc.*.sock | head -1)
export YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/run/ydotoold.socket}
export LAB_SAY_ECHO=1
here=$(cd "$(dirname "$0")" && pwd)
log=$HOME/.local/state/basalt-shell/audit.jsonl
monitor=$(pactl get-default-sink).monitor
shot() { grim "$out/$1.png"; echo "shot $1"; }
rec_start() { wf-recorder -y -a="$monitor" -c libvpx -p deadline=good -p cpu-used=4 -p b=4M -C libopus -f "$out/$1.webm" >/dev/null 2>&1 & REC=$!; sleep 1.5; }
rec_stop() { sleep 1.5; kill -INT "$REC" 2>/dev/null; for _ in $(seq 30); do kill -0 "$REC" 2>/dev/null || break; sleep 0.5; done; wait "$REC" 2>/dev/null; echo "rec $1"; }
ui() { basalt-shell-ui ipc call shell "$@" >/dev/null; }
lines() { wc -l <"$log"; }
# wait_for N PATTERN: a new record (after line N) matching PATTERN.
wait_for() {
  for _ in $(seq 400); do
    tail -n +$(($1 + 1)) "$log" | grep -q -- "$2" && return 0
    sleep 0.5
  done
  echo "timeout waiting for $2" >&2
}
click_primary() { # click the primary button on screen (Allow, Send, Insert)
  local tmp=/tmp/demo-find.png xy
  grim "$tmp"
  xy=$(python3 "$here/findbtn.py" "$tmp" "$@") || { echo "no button" >&2; return 1; }
  # A person's pointer: move there, a short pause, click.
  ydotool mousemove -a -x ${xy% *} -y ${xy#* } >/dev/null; sleep 0.6
  ydotool click 0xC0 >/dev/null
}
say() { # say TEXT NAME: a spoken request, screenshot while the key is held
  ( sleep 1.6; shot "$2-listening" ) &
  lab-say --text "$1"
}

scene_files() {
  ui close; sleep 0.5
  rec_start files
  local n; n=$(lines)
  say "Find the PDF the bank sent last month." files-1
  wait_for "$n" '"need_grant"'; sleep 2.5; shot files-2-permission
  n=$(lines); click_primary
  wait_for "$n" '"answer"'; sleep 7
  shot files-3-answer
  rec_stop files
}
scene_mail() {
  ui close; sleep 0.5
  rec_start mail-summary
  local n; n=$(lines)
  say "What did Priya say in her last email?" mail-1
  wait_for "$n" '"need_grant"'; sleep 2.5; shot mail-2-permission
  n=$(lines); click_primary
  wait_for "$n" '"type":"voice","actor":"voice"\|"answer"'; wait_for "$n" '"answer"'; sleep 9
  shot mail-3-summary
  rec_stop mail-summary
}
scene_reply() {
  ui close; sleep 0.5
  rec_start reply
  local n; n=$(lines)
  say "Reply to Priya: the slides will be ready on Friday morning." reply-1
  wait_for "$n" '"act":"mail.send"'; sleep 6
  shot reply-2-draft
  n=$(lines); click_primary
  wait_for "$n" '"type":"done"'; sleep 3
  shot reply-3-sent
  rec_stop reply
}
scene_invoice() {
  ui close; sleep 0.5
  rec_start malicious-email
  local n; n=$(lines)
  say "Summarize the email about the overdue invoice." invoice-1
  wait_for "$n" '"answer"'; sleep 12
  shot invoice-2-flagged
  rec_stop malicious-email
}
scene_timeline() {
  ui close; sleep 0.5
  rec_start timeline
  ui open activity; sleep 4
  shot timeline-1
  ydotool mousemove -a -x 1700 -y 600 >/dev/null; sleep 0.5
  for _ in 1 2 3 4; do ydotool mousemove -w -x 0 -y -3 >/dev/null; sleep 1.2; done
  shot timeline-2
  sleep 1; ui close
  rec_stop timeline
}
scene_dictation() {
  ui close; sleep 0.5
  swaymsg -q '[app_id="org.xfce.mousepad"] kill' 2>/dev/null; sleep 0.5
  swaymsg -q exec mousepad; sleep 3
  rec_start dictation
  local n; n=$(lines)
  say "Hi Ana, Thursday at twelve thirty works for me. See you at the Italian place." dictation-1
  wait_for "$n" 'dictation shown for confirmation'; sleep 3
  shot dictation-2-preview
  n=$(lines); click_primary --region 600,60,1320,260
  wait_for "$n" '"type":"done"'; sleep 2.5
  shot dictation-3-typed
  rec_stop dictation
}
scenes=${*:-files mail reply invoice timeline dictation}
for s in $scenes; do "scene_$s"; done
