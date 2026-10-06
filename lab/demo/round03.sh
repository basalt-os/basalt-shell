#!/bin/bash
# Lab VM (desktop user, inside the running Basalt session): the 0.3 visual
# round. Normal app windows with the new decorations, window states, the
# taskbar, GTK 4 / libadwaita and Qt apps in light and dark, launcher,
# command bar, assistant report; PNGs (grim) and short WebM clips
# (wf-recorder) into OUTDIR.
#
#   round03.sh OUTDIR [scene ...]
#   scenes: desktop states menu toolkits dark launcher commandbar assistant clips
#
# Input is synthetic (wtype, ydotool through the lab's ydotoold). The
# assistant scene needs a broken nginx (break-nginx.sh as root).
# BASALT_ROUND_OVERRIDES='{"window.blur": true}' adds token overrides to
# the clean theme (the SwayFX set).
set -u
mkdir -p ~/vr/ffprofile
out=${1:?output directory}; shift
mkdir -p "$out"
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
export WAYLAND_DISPLAY=$(ls "$XDG_RUNTIME_DIR" | grep -E '^wayland-[0-9]+$' | head -1)
export SWAYSOCK=$(ls -t "$XDG_RUNTIME_DIR"/sway-ipc.*.sock 2>/dev/null | head -1)
export NIRI_SOCKET=$(ls -t "$XDG_RUNTIME_DIR"/niri.*.sock 2>/dev/null | head -1)
if [ -n "$SWAYSOCK" ] && [ -n "$NIRI_SOCKET" ]; then
  if [ "$SWAYSOCK" -nt "$NIRI_SOCKET" ]; then unset NIRI_SOCKET; else unset SWAYSOCK; fi
fi
[ -z "${SWAYSOCK:-}" ] && unset SWAYSOCK; [ -z "${NIRI_SOCKET:-}" ] && unset NIRI_SOCKET
export YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/run/ydotoold.socket}
comp=niri; [ -n "${SWAYSOCK:-}" ] && comp=sway

ui() { basalt-shell-ui ipc call shell "$@" >/dev/null; }
shot() { sleep 0.4; grim "$out/$1.png"; echo "shot $1"; }
rec_start() { wf-recorder -y -c libvpx -p deadline=realtime -p cpu-used=8 -p b=4M -f "$out/$1.webm" >/dev/null 2>&1 & REC=$!; sleep 1.2; }
rec_stop() {
  sleep 0.8; kill -INT "$REC" 2>/dev/null
  for _ in $(seq 20); do kill -0 "$REC" 2>/dev/null || break; sleep 0.5; done
  kill -TERM "$REC" 2>/dev/null; wait "$REC" 2>/dev/null; echo "rec done"
}
key() { wtype -k "$1"; sleep "${2:-0.4}"; }
superkey() { ydotool key 125:1 "$1":1 "$1":0 125:0 >/dev/null; sleep "${2:-1.2}"; }
type_slow() { wtype -d 45 "$1"; }
mv_() { ydotool mousemove -a -x "$1" -y "$2" >/dev/null; sleep 0.05; }
click() { mv_ "$1" "$2"; sleep 0.25; ydotool click 0xC0 >/dev/null; sleep "${3:-0.6}"; }
drag() { # x1 y1 x2 y2 [down up]
  mv_ "$1" "$2"; sleep 0.3; ydotool click "${5:-0x40}" >/dev/null; sleep 0.2
  local n=24 px=0 py=0 nx ny i
  for i in $(seq 1 $n); do nx=$(( ($3-$1)*i/n )); ny=$(( ($4-$2)*i/n )); ydotool mousemove -x $((nx-px)) -y $((ny-py)) >/dev/null; px=$nx; py=$ny; sleep 0.04; done
  ydotool click "${6:-0x80}" >/dev/null; sleep 0.8
}
spawn() { if [ "$comp" = sway ]; then swaymsg -q exec "$*"; else niri msg action spawn -- sh -c "$*"; fi; }
close_all() {
  if [ "$comp" = sway ]; then swaymsg -q '[all] kill' 2>/dev/null
  else for w in $(niri msg --json windows | jq -r '.[].id'); do niri msg action close-window --id "$w"; done; fi
  pkill -x gnome-text-edit; pkill -x nautilus
  for _ in $(seq 20); do pgrep -x firefox >/dev/null || break; sleep 0.5; done; sleep 1
}
rect() { basalt-shell ctl desktop | jq -r --arg a "$1" '.windows[] | select(.app_id==$a) | "\(.rect.x) \(.rect.y) \(.rect.width) \(.rect.height)"' | head -1; }
wait_app() { for _ in $(seq 40); do [ -n "$(rect "$1")" ] && return 0; sleep 0.5; done; echo "no window $1" >&2; }
# propose ACTION JSON: a typed action, confirmed on the sheet with Right
# (the sheet opens on Decline) and Enter, again if it was not focused yet.
propose() {
  basalt-shell propose "$1" "$2" --wait 30 >/dev/null 2>&1 & local pid=$!
  sleep 1.4
  for _ in 1 2 3 4; do key Right 0.3; key Return 1; kill -0 "$pid" 2>/dev/null || return 0; done
  wait "$pid"
}
place() { propose window.move "{\"window\":\"$1\",\"x\":$2,\"y\":$3,\"width\":$4,\"height\":$5}"; }
state() { propose window.set_state "{\"window\":\"$1\",\"state\":\"$2\"}"; sleep 0.5; }
focus() { propose window.focus "{\"window\":\"$1\"}"; }
settings() { # MODE
  local o=${BASALT_ROUND_OVERRIDES:-{\}}
  jq -n --arg m "$1" --argjson o "$o" '{theme: "basalt", mode: $m, motion: "auto", overrides: $o}' >~/.config/basalt-shell/settings.json
  pkill -x basalt-shelld; sleep 1; spawn basalt-shelld; sleep 4
}

open_three() {
  close_all
  mkdir -p ~/Documents ~/Pictures ~/Downloads
  [ -f ~/Documents/notes.txt ] || printf 'Basalt OS\n\nShell 0.3: title bars, window buttons, taskbar.\n' >~/Documents/notes.txt
  spawn "foot sh -c 'cat /etc/os-release | head -4; echo; uname -r; exec bash'"; wait_app foot
  spawn "nautilus $HOME"; wait_app org.gnome.Nautilus
  spawn "firefox --new-instance --profile $HOME/vr/ffprofile https://basalt-os.org"; wait_app org.mozilla.firefox
  sleep 5
  place org.mozilla.firefox 560 90 1240 800
  place org.gnome.Nautilus 120 170 860 560
  place foot 260 640 820 380
}

scene_desktop() {
  settings light
  open_three
  focus org.gnome.Nautilus
  shot 01-desktop-three-apps
}

scene_states() {
  # The same calls Super+Up, Super+Down, Super+Left and Super+H make.
  focus org.gnome.Nautilus
  ui window maximize; sleep 2
  shot 02-maximized
  ui window restore; sleep 2
  shot 03-restored
  ui window left; sleep 2
  shot 04-snap-left
  ui window restore; sleep 2
  focus org.mozilla.firefox
  ui window minimize; sleep 2
  shot 05-minimized-taskbar
  state org.mozilla.firefox normal
  shot 06-restored-from-taskbar
}

scene_menu() {
  focus foot
  ydotool key 125:1 56:1 57:1 57:0 56:0 125:0 >/dev/null; sleep 1.5   # Super+Alt+Space
  shot 07-window-menu
  key Escape 0.6; ui close; sleep 0.6
}

scene_toolkits() {
  for m in light dark; do
    settings "$m"
    close_all
    spawn "gnome-text-editor --standalone $HOME/Documents/notes.txt"; wait_app org.gnome.TextEditor
    spawn "featherpad $HOME/Documents/notes.txt"; sleep 3
    spawn keepassxc; sleep 4
    local fp; fp=$(basalt-shell ctl desktop | jq -r '.windows[] | select(.app_id|test("featherpad";"i")) | .app_id' | head -1)
    local kp; kp=$(basalt-shell ctl desktop | jq -r '.windows[] | select(.app_id|test("keepassxc";"i")) | .app_id' | head -1)
    place org.gnome.TextEditor 80 110 860 560
    [ -n "$fp" ] && place "$fp" 980 110 860 560
    [ -n "$kp" ] && place "$kp" 520 600 900 430
    focus org.gnome.TextEditor
    shot "08-gtk4-qt-$m"
  done
}

scene_dark() {
  settings dark
  open_three
  focus org.mozilla.firefox
  shot 09-desktop-dark
}

scene_launcher() {
  settings light
  open_three
  ui toggle launcher; sleep 1.5
  shot 10-launcher
  type_slow "text"; sleep 1
  shot 11-launcher-search
  key Escape 0.6; ui close; sleep 0.6
}

scene_commandbar() {
  ui open commandbar; sleep 1
  type_slow "make it darker with rounder corners"; sleep 0.5
  key Return 2.5
  shot 12-commandbar-proposal
  key Escape 0.5; ui close; sleep 0.8
}

scene_assistant() {
  ui open commandbar; sleep 1
  type_slow "why nginx"; sleep 0.4
  key Return 8
  shot 13-assistant-report
  key Escape 0.5; ui close; sleep 0.8
}

scene_clips() {
  settings light
  close_all
  spawn "gnome-text-editor --standalone"; wait_app org.gnome.TextEditor
  spawn "foot"; wait_app foot
  spawn "firefox --new-instance --profile $HOME/vr/ffprofile https://basalt-os.org"; wait_app org.mozilla.firefox; sleep 4
  place org.gnome.TextEditor 120 140 760 480
  place foot 1000 200 760 480
  place org.mozilla.firefox 400 520 900 500
  rec_start window-controls
  local x y w h
  read -r x y w h <<<"$(rect org.gnome.TextEditor)"
  drag $((x+w-180)) $((y+20)) $((x+w+40)) $((y+120))       # move by the headerbar
  read -r x y w h <<<"$(rect org.gnome.TextEditor)"
  drag $((x+w+3)) $((y+h+3)) $((x+w+163)) $((y+h+83))       # resize from the corner
  read -r x y w h <<<"$(rect foot)"
  ydotool key 125:1 >/dev/null; drag $((x+300)) $((y+200)) $((x+200)) $((y+120)); ydotool key 125:0 >/dev/null   # Super+drag
  read -r x y w h <<<"$(rect foot)"
  ydotool key 125:1 >/dev/null; drag $((x+w-40)) $((y+h-40)) $((x+w-160)) $((y+h-110)) 0x41 0x81; ydotool key 125:0 >/dev/null  # Super+right-drag resize
  focus org.mozilla.firefox
  superkey 103 1.6; superkey 108 1.6     # maximize, restore
  superkey 105 1.6; superkey 106 1.6     # snap left, right
  superkey 108 1.6                       # restore
  superkey 35 2                          # Super+H minimize
  state org.mozilla.firefox normal
  rec_stop
  rec_start launcher-commandbar
  ui toggle launcher; sleep 1.2; type_slow "files"; sleep 1; key Return 3
  ui open commandbar; sleep 1; type_slow "use the lichen theme"; key Return 2; key Left 0.4; key Return 3
  ui close; sleep 1.5
  ui open commandbar; sleep 1; type_slow "light mode"; key Return 2; key Left 0.4; key Return 3
  ui close; sleep 1
  rec_stop
  settings light
}

scenes=${*:-desktop states menu toolkits dark launcher commandbar assistant clips}
for s in $scenes; do "scene_$s"; done
close_all
ls -la "$out"
