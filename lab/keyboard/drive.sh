#!/bin/bash
# Inside the test container, after session.sh: drive the shell with the
# keyboard only (wtype, through the compositor) and check after each step
# which control holds the keyboard (the shell's IPC: focused, surfaces).
# Screenshots of the focus rings go to /out. Exit status 1 when a check
# fails. Run by lab/keyboard/run.sh.
set -uo pipefail
export XDG_RUNTIME_DIR=/tmp/xdg WAYLAND_DISPLAY=wayland-1 PATH=/tmp/stage/usr/bin:$PATH
export BASALT_SHELL_SOCKET=$XDG_RUNTIME_DIR/basalt-shell-headless/shell.sock
SWAYSOCK=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'sway-ipc.*.sock' 2>/dev/null | head -1)
export SWAYSOCK
qml=/tmp/stage/usr/share/basalt-shell/qml
pass=0
fail=0

ui() { qs -p "$qml" ipc --any-display call shell "$@" 2>/dev/null; }
# A keyboard that stays for the whole run: each wtype adds a virtual
# keyboard of its own and removes it when it ends; with one that stays,
# the seat's keymap and the focused surface do not change between keys.
wtype -k XF86WakeUp -s 900000 -k XF86WakeUp &
keeper=$!
trap 'kill $keeper 2>/dev/null' EXIT
sleep 1
# k KEY [KEY]: each one key (wtype -k) or a chord ("ctrl+alt+Tab", "logo+comma").
k() {
  local spec
  for spec in "$@"; do
    local mods=() key=$spec
    while [[ $key == *+* ]]; do mods+=("${key%%+*}"); key=${key#*+}; done
    local args=()
    for m in "${mods[@]}"; do args+=(-M "$m"); done
    args+=(-k "$key")
    for m in "${mods[@]}"; do args+=(-m "$m"); done
    wtype "${args[@]}"
    sleep 0.45
  done
}
type_text() { wtype "$1"; sleep 0.4; }
shot() { sleep 0.3; grim "/out/$1.png"; }
# expect PATTERN [what]: the focused control matches the shell pattern.
expect() {
  local got
  got=$(ui focused)
  # shellcheck disable=SC2053 # pattern on purpose
  if [[ $got == $1 ]]; then pass=$((pass + 1)); echo "ok    ${2:-focus} = $got"
  else fail=$((fail + 1)); echo "FAIL  ${2:-focus}: want $1, got '$got' (surfaces: $(ui surfaces))"; fi
}
# surfaces PATTERN: the open surfaces match.
surfaces() {
  local got
  got=$(ui surfaces)
  # shellcheck disable=SC2053
  if [[ $got == $1 ]]; then pass=$((pass + 1)); echo "ok    surfaces = '$got'"
  else fail=$((fail + 1)); echo "FAIL  surfaces: want '$1', got '$got'"; fi
}
check() {
  if eval "$1"; then pass=$((pass + 1)); echo "ok    $2"; else fail=$((fail + 1)); echo "FAIL  $2"; fi
}
# last_decision: the type of the newest confirm or decline record in the
# activity log (the person's last answer to a proposal).
last_decision() {
  basalt-shell ctl activity '{"n":20}' 2>/dev/null | grep -oE '"type": ?"(confirm|decline)"' | tail -1 | grep -oE 'confirm|decline'
}
decided() { [ "$(last_decision)" = "$1" ]; }

echo "== Settings: sidebar, pages, groups, slider, back"
k logo+comma; sleep 1
surfaces "settings page:appearance"
expect settings-nav-appearance "Settings opens on the current page's sidebar entry"
shot 01-settings-sidebar
k Down
expect settings-nav-tokens "Down moves along the sidebar"
surfaces "settings page:tokens"
k End
expect settings-nav-about "End"
k Home
expect settings-nav-appearance "Home"
k w
expect settings-nav-windows "type-ahead w"
k Home Right
expect "settings-theme-*" "Right goes into the page (theme cards)"
shot 02-settings-theme-card
k Right
expect "settings-theme-*" "Right moves along the theme cards"
k Left Left
expect settings-nav-appearance "Left at the first card returns to the sidebar"
k Tab
expect "settings-theme-*" "Tab from the sidebar goes into the page"
k Tab
expect "settings-mode-*" "Tab: the mode group (one stop)"
shot 03-settings-mode
k Tab
expect "settings-accent-*" "Tab: the accent swatches"
k Right
expect "settings-accent-c0392b" "Right moves along the swatches"
shot 04-settings-accent
k Tab
expect settings-radius "Tab: the corner slider"
shot 05-settings-slider
k shift+Tab
expect "settings-accent-c0392b" "Shift+Tab returns to the swatch focused last (roving stop)"
k Escape
expect settings-nav-appearance "Escape in the page returns to the sidebar"
for _ in 1 2 3 4 5; do k Down; done
expect settings-nav-voice "Down to Voice and assistant"
sleep 1
k Tab
expect "?*" "Tab into the Voice page reaches a control"
shot 06-settings-voice
k Escape
k a
expect settings-nav-drivers "type-ahead a: the next entry with A after Voice (Additional drivers)"
k a a
expect settings-nav-appearance "type-ahead a cycles (About, then Appearance)"
k a a
expect settings-nav-drivers "type-ahead a again"
sleep 1
shot 07-settings-drivers
k ctrl+w
sleep 0.5
surfaces ""

echo "== Quick settings and the power menu"
k logo+s; sleep 0.8
surfaces quicksettings
expect quick-mode "Quick settings opens on the first tile"
shot 08-quick-settings
k Right
expect quick-motion "Right in the tiles"
k Left Down
expect "quick-network" "Down moves a row in the tiles"
k Tab
expect "quick-theme-*" "Tab: the themes"
k Tab
expect "qs-settings" "Tab: the bottom buttons"
k Tab Tab
expect qs-power "Tab to the power button"
k Return; sleep 0.6
surfaces power
expect power-lock "The power menu opens on its first entry"
k Down
expect power-logout "Down in the power menu"
shot 09-power-menu
k Return; sleep 0.6
expect power-cancel "The log out countdown opens on Cancel, not Log out"
shot 09b-power-countdown
k Left
expect power-confirm "Left reaches Log out (visual order)"
k Right
expect power-cancel "Right back to Cancel"
k Return; sleep 0.6
surfaces quicksettings
expect qs-power "Return on Cancel closes and returns to the quick settings power button"
k Return; sleep 0.6
surfaces power
k Escape; sleep 0.6
surfaces quicksettings
expect qs-power "Escape returns to the quick settings power button"
k Escape; sleep 0.5
surfaces ""

echo "== The panel"
k ctrl+alt+Tab; sleep 0.6
surfaces panel
expect panel-launcher "Ctrl+Alt+Tab puts the keyboard on the panel"
k Right
expect "panel-ws-*" "Right: the workspaces"
shot 10-panel
k End
expect panel-power "End: the power button"
k Return; sleep 0.6
surfaces power
expect power-lock "The power menu from the panel"
k Escape; sleep 0.6
surfaces panel
expect panel-power "Escape returns to the panel's power button"
k Left
expect panel-quicksettings "Left along the panel"
k Return; sleep 0.6
expect quick-mode "Quick settings from the panel"
k Escape; sleep 0.6
expect panel-quicksettings "Escape returns to the panel"
k Escape; sleep 0.4
surfaces ""

echo "== Window menu (a terminal)"
swaymsg -q exec foot; sleep 2
k logo+alt+space; sleep 0.8
surfaces windowmenu
expect "window-menu-*" "The window menu opens on its first entry"
k Down
shot 11-window-menu
k Escape; sleep 0.4
surfaces ""

echo "== Launcher"
k logo+space; sleep 0.8
expect launcher-search "The launcher opens on its search field"
type_text "fo"
k Down
shot 12-launcher
k Escape; sleep 0.4
surfaces ""

echo "== Command bar"
k logo+a; sleep 0.8
expect commandbar-field "The command bar opens on its field"
k Tab
expect "make it darker*" "Tab: the suggestions"
k Right
shot 13-commandbar
k Escape; sleep 0.4
surfaces ""

echo "== A proposal in the command bar: it opens on Ignore, Return declines"
k logo+a; sleep 0.8
type_text "light mode"
k Return; sleep 1.5
expect proposal-decline "The proposal focuses Ignore, not Apply"
shot 13b-commandbar-proposal
k Left
expect proposal-confirm "Left reaches Apply (visual order)"
k Right
expect proposal-decline "Right back to Ignore"
k Return; sleep 0.8
check "decided decline" "Return on the opened proposal declined it (last record: $(last_decision))"
k Escape; sleep 0.4
surfaces ""

echo "== A proposal from an agent: it opens on Decline; Return declines, Right and Return confirm, Escape declines"
basalt-shell propose theme.switch '{"mode":"light"}' >/tmp/p0.out 2>&1 &
p0=$!
sleep 1.2
expect confirm-decline "The sheet focuses Decline, not Confirm"
shot 14-confirm-sheet
k Return
wait $p0
check 'grep -q declined /tmp/p0.out' "Return right after opening declined the proposal ($(tr -d '\n' </tmp/p0.out | head -c 80))"
basalt-shell propose theme.switch '{"mode":"light"}' >/tmp/p1.out 2>&1 &
p1=$!
sleep 1.2
expect confirm-decline "The sheet focuses Decline again"
k Right
expect confirm-allow "Right moves to Confirm (visual order)"
shot 14b-confirm-sheet-confirm
k Return
wait $p1
check 'grep -q applied /tmp/p1.out' "Right and Return confirmed the proposal ($(tr -d '\n' </tmp/p1.out | head -c 80))"
sleep 1
shot 15-light-mode-settings-after
basalt-shell propose theme.switch '{"mode":"dark"}' >/tmp/p2.out 2>&1 &
p2=$!
sleep 1.2
k Escape
wait $p2
check 'grep -q declined /tmp/p2.out' "Escape declined the proposal ($(tr -d '\n' </tmp/p2.out | head -c 80))"

echo "== A question with fixed options (the screen-share chooser): it opens on Cancel"
basalt-shell choose-output >/tmp/c0.out 2>&1 &
c0=$!
sleep 1.2
expect chooser-cancel "The chooser focuses Cancel, not an option"
shot 15b-chooser
k Return
wait $c0
check '[ ! -s /tmp/c0.out ] || ! grep -q HEADLESS /tmp/c0.out' "Return right after opening cancelled the choice (nothing chosen)"
basalt-shell choose-output >/tmp/c1.out 2>&1 &
c1=$!
sleep 1.2
k Escape
wait $c1
check '[ ! -s /tmp/c1.out ] || ! grep -q HEADLESS /tmp/c1.out' "Escape cancelled the choice (nothing chosen)"
basalt-shell choose-output >/tmp/c2.out 2>&1 &
c2=$!
sleep 1.2
k shift+Tab
expect "chooser-HEADLESS*" "Shift+Tab reaches the options"
k Return
wait $c2
check 'grep -q HEADLESS /tmp/c2.out' "Shift+Tab and Return chose an option ($(tr -d '\n' </tmp/c2.out | head -c 60))"

echo "== Light mode: the rings on light surfaces"
k logo+s; sleep 0.8
expect quick-mode "Quick settings (light)"
k Tab
shot 16-quick-settings-light
k Escape; sleep 0.4
k logo+comma; sleep 1
k Right Tab
shot 17-settings-light
k ctrl+w; sleep 0.4

echo "== Notifications and activity"
k logo+n; sleep 0.8
expect drawer-tab-notifications "The drawer opens on its tab"
k Right
expect drawer-tab-activity "Right: the Activity tab"
surfaces activity
k Tab
shot 18-activity
k Escape; sleep 0.4
surfaces ""

echo "== Twenty windows: new windows below the panel, the window list clear of the clock"
# A window as large as the output: sway centers it on the whole output,
# under the panel; the daemon moves it into the usable area.
swaymsg -q exec "foot --title big --window-size-pixels=1600x1000"
sleep 6
# top of the frame (title bar included), workspace top, frame bottom, workspace bottom
read -r wtop atop wbot abot < <(swaymsg -t get_tree | jq -r '.. | objects | select(.type == "workspace" and .name != "__i3_scratch") as $ws
  | $ws.floating_nodes[]? | select(.name == "big")
  | [(.rect.y - (if .border == "normal" then .deco_rect.height else 0 end)), $ws.rect.y, (.rect.y + .rect.height), ($ws.rect.y + $ws.rect.height)] | @tsv' | head -1)
# shellcheck disable=SC2016 # check evaluates it
check '[ -n "${wtop:-}" ] && [ "$wtop" -ge "$atop" ] && [ "$wbot" -le "$abot" ]' \
  "A screen-sized window is kept below the panel (frame ${wtop:-?}..${wbot:-?}, usable ${atop:-?}..${abot:-?})"
count_windows() { swaymsg -t get_tree | jq '[.. | objects | select(.app_id == "foot")] | length'; }
have=$(count_windows)
for i in $(seq 1 $((20 - have))); do swaymsg -q exec "foot --title w$i"; sleep 0.3; done
sleep 4
n=$(count_windows)
# shellcheck disable=SC2016 # check evaluates it
check '[ "$n" -ge 20 ]' "20 windows open ($n)"
shot 19-panel-twenty-windows
k ctrl+alt+Tab; sleep 0.6
expect panel-launcher "The panel takes the keyboard with $n windows"
# Right along the panel: every window's entry is a stop, each drawn left
# of the clock (the list scrolls to the focused entry), then the clock.
rects=()
for _ in $(seq 1 $((n + 10))); do
  k Right
  name=$(ui focused)
  [[ $name == task-* ]] && rects+=("$(ui focusedRect)")
  [[ $name == panel-clock ]] && break
done
expect panel-clock "Right after the last window's entry reaches the clock"
# shellcheck disable=SC2016 # check evaluates it
check '[ "${#rects[@]}" -eq "$n" ]' "Right reaches each of the $n entries (${#rects[@]})"
read -r cx _ < <(ui focusedRect)
over=0
for r in "${rects[@]}"; do
  read -r x _ w _ <<<"$r"
  if [ "$x" -lt 0 ] || [ $((x + w)) -gt "${cx:-0}" ]; then over=$((over + 1)); echo "      entry at $r runs under the clock at x=$cx"; fi
done
# shellcheck disable=SC2016 # check evaluates it
check '[ -n "${cx:-}" ] && [ "$over" -eq 0 ]' "No entry overlaps the clock (clock at x=${cx:-?}, last entry ${rects[-1]:-none})"
k Left
shot 20-panel-last-entry
expect "task-*" "Left from the clock: the last entry, scrolled into view"
k Escape; sleep 0.4
surfaces ""

echo
echo "passed $pass, failed $fail"
[ "$fail" -eq 0 ]
