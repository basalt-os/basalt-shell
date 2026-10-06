#!/bin/bash
# Lab VM (desktop user, inside the running Basalt session): reproduce the
# demo scenes and capture screenshots (grim) and short recordings
# (wf-recorder, VP8 WebM) into OUTDIR.
#
#   capture.sh OUTDIR [scene ...]
#   scenes: reset panel launcher notifications settings commandbar assistant mcp apps
#
# Input is synthetic: keyboard through wtype (virtual keyboard protocol),
# pointer through ydotool (uinput; the lab runs ydotoold and sets a flat
# pointer profile so absolute moves land where asked). The assistant scene
# needs a broken nginx (lab/demo/break-nginx.sh as root) and the desktop
# user's password in $BASALT_LAB_PASSWORD_FILE.
set -u
out=${1:?output directory}; shift
mkdir -p "$out"
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
export WAYLAND_DISPLAY=$(ls "$XDG_RUNTIME_DIR" | grep -E '^wayland-[0-9]+$' | head -1)
# Newest sockets: earlier sessions can leave stale ones behind.
export SWAYSOCK=$(ls -t "$XDG_RUNTIME_DIR"/sway-ipc.*.sock 2>/dev/null | head -1)
export NIRI_SOCKET=$(ls -t "$XDG_RUNTIME_DIR"/niri.*.sock 2>/dev/null | head -1)
if [ -n "$SWAYSOCK" ] && [ -n "$NIRI_SOCKET" ]; then
  if [ "$SWAYSOCK" -nt "$NIRI_SOCKET" ]; then unset NIRI_SOCKET; else unset SWAYSOCK; fi
fi
export YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/run/ydotoold.socket}
here=$(cd "$(dirname "$0")" && pwd)
comp=niri; [ -n "${SWAYSOCK:-}" ] && comp=sway

ui() { basalt-shell-ui ipc call shell "$@" >/dev/null; }
shot() { grim "$out/$1.png"; echo "shot $1"; }
rec_start() { wf-recorder -y -c libvpx -p deadline=good -p cpu-used=4 -p b=5M -f "$out/$1.webm" >/dev/null 2>&1 & REC=$!; sleep 1; }
rec_stop() {
  sleep 0.8
  kill -INT "$REC" 2>/dev/null
  for _ in $(seq 20); do kill -0 "$REC" 2>/dev/null || break; sleep 0.5; done
  kill -TERM "$REC" 2>/dev/null
  wait "$REC" 2>/dev/null
  echo "rec done"
}
click() { ydotool mousemove -a -x "$1" -y "$2" >/dev/null; sleep 0.25; ydotool click 0xC0 >/dev/null; sleep "${3:-0.6}"; }
key() { wtype -k "$1"; sleep "${2:-0.4}"; }
type_slow() { wtype -d 45 "$1"; }
spawn() { if [ "$comp" = sway ]; then swaymsg -q exec "$*"; else niri msg action spawn -- sh -c "$*"; fi; }
close_all() {
  if [ "$comp" = sway ]; then swaymsg -q '[all] kill' 2>/dev/null
  else for w in $(niri msg --json windows | jq -r '.[].id'); do niri msg action close-window --id "$w"; done; fi
  sleep 1
}
win_rect() { basalt-shell ctl desktop | jq -r --arg t "$1" '.windows[] | select(.title==$t) | "\(.rect.x) \(.rect.y)"' | head -1; }
# Click at a point relative to the settings window (looked up each time:
# the window moves when the panel changes sides).
sclick() {
  set -- "$1" "$2" $(win_rect "Basalt Settings")
  click $(( ${3:-470} + $1 )) $(( ${4:-190} + $2 )) 2.2
}

scene_reset() {
  close_all
  pkill -x gnome-text-edit; pkill -x nautilus
  rm -f ~/.config/basalt-shell/settings.json
  rm -rf ~/.local/share/gnome-text-editor
  # Restart daemon and UI on a clean theme and activity log.
  pkill -x basalt-shelld; pkill -x qs; sleep 1
  rm -f ~/.local/state/basalt-shell/audit.jsonl
  spawn basalt-shelld; sleep 1.5; spawn basalt-shell-ui; sleep 4
}

scene_panel() {
  spawn foot; sleep 1; spawn gnome-text-editor ~/Documents/notes.txt; sleep 1; spawn nautilus; sleep 3
  shot 01-panel-floating
  # Arranging is a typed action too: through the command bar, confirmed.
  ui open commandbar; sleep 1
  type_slow "cascade windows"; key Return 1.5
  key Left 0.4; key Return 2.5
  ui close; sleep 1
  shot 01-panel
}

scene_launcher() {
  rec_start 02-launcher
  ui toggle launcher; sleep 1.2
  shot 02-launcher-open
  type_slow "text"; sleep 1
  shot 02-launcher-search
  key Down 0.5; key Up 0.5
  key Return 3
  rec_stop
}

scene_notifications() {
  rec_start 03-notifications
  notify-send -a "Software" -i system-software-update "Updates available" "12 packages can be updated. A snapshot is taken before and after."
  sleep 1.2
  notify-send -a "Backup" -u critical "Backup disk is almost full" "92 % used on /srv/backup"
  sleep 2
  shot 03-notification-popups
  ui open notifications; sleep 1.5
  shot 03-notification-center
  ui close; sleep 1
  ui dismissPopups
  rec_stop
}

scene_settings() {
  rec_start 04-settings-themes
  ui open quicksettings; sleep 1.5
  shot 04-quick-settings
  ui close; sleep 0.6
  ui settingsPage appearance; sleep 2.5
  shot 04-settings-appearance
  sclick 565 188      # Lichen
  shot 04-settings-lichen-dark
  sclick 292 329      # Light
  shot 04-settings-lichen-light
  sclick 777 188      # Tide (panel moves to the bottom)
  shot 04-settings-tide-light
  sclick 376 329      # Dark
  sclick 352 188      # Basalt
  ui settingsPage tokens; sleep 2
  shot 04-settings-tokens
  ui settingsPage motion; sleep 2.5
  shot 04-settings-motion
  ui settingsPage ai; sleep 1.5
  shot 04-settings-ai
  ui toggle settings; sleep 1
  rec_stop
}

scene_commandbar() {
  rec_start 05-commandbar-theme
  ui open commandbar; sleep 1
  type_slow "make it darker with rounder corners"; sleep 0.5
  key Return 2
  shot 05-commandbar-proposal
  key Left 0.4   # the proposal opens on Ignore: Left to Apply
  key Return 2.5
  shot 05-commandbar-applied
  ui close; sleep 1.5
  shot 05-desktop-after
  rec_stop
}

scene_assistant() {
  rec_start 06-assistant-proposal
  ui open commandbar; sleep 1
  type_slow "why nginx"; sleep 0.4
  key Return 6
  shot 06-assistant-report
  key Left 0.4   # the proposal opens on Ignore: Left to Apply
  key Return 3
  shot 06-polkit
  if [ -r "${BASALT_LAB_PASSWORD_FILE:-}" ]; then
    wtype "$(cat "$BASALT_LAB_PASSWORD_FILE")"; key Return 10
    shot 06-assistant-applied
  fi
  ui close; sleep 1
  rec_stop
}

scene_mcp() {
  rec_start 07-mcp-confirm
  ( "$here/mcp-call.py" theme_set_tokens '{"tokens": {"color.accent": "#2f7d78", "radius.window": 18, "radius.md": 16, "radius.lg": 24}}' >"$out/07-mcp-result.txt" 2>&1 & )
  sleep 3
  shot 07-mcp-sheet
  key Right 0.4  # the sheet opens on Decline: Right to Confirm
  key Return 3
  shot 07-mcp-applied
  ui open activity; sleep 2
  shot 07-activity
  ui close; sleep 1
  rec_stop
}

scene_apps() {
  close_all
  for a in gnome-text-editor mousepad featherpad keepassxc firefox xterm xeyes; do spawn "$a"; sleep 1.5; done
  sleep 6
  ui open commandbar; sleep 1
  type_slow "arrange windows in a grid"; key Return 1.5
  key Left 0.4; key Return 3
  ui close; sleep 1
  shot 08-apps
}

scenes=${*:-reset panel launcher notifications settings commandbar mcp}
for s in $scenes; do "scene_$s"; done
ls -la "$out"
