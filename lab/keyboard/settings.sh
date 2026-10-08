#!/bin/bash
# Inside the test container, after session.sh (and drive.sh): Settings,
# Keyboard with the keyboard alone. Adds layouts through the picker,
# reorders and removes one, picks a switch key and a Caps Lock behavior,
# types in the try field, then starts a new session (with a stand-in for
# the system assistant's command line) and checks that the settings came
# back and that "Use my layouts there too" opens its sheet on Not now.
# Screenshots go to /out/kbd-*.png. Exit status 1 when a check fails.
#
# What a headless sway can and cannot show: it has no real keyboard, and
# virtual keyboards (wtype) keep their own keymap, so the characters typed
# here do not depend on the layouts (the layouts' effect on a real
# keyboard is checked in a VM, see README.md). The checks read what the
# shell wrote and what sway accepted.
set -uo pipefail
export XDG_RUNTIME_DIR=/tmp/xdg WAYLAND_DISPLAY=wayland-1 PATH=/tmp/stage/usr/bin:$PATH LANG=C.UTF-8
export BASALT_SHELL_SOCKET=$XDG_RUNTIME_DIR/basalt-shell-headless/shell.sock
export BASALT_SHELL_DATA=/tmp/stage/usr/share/basalt-shell
qml=/tmp/stage/usr/share/basalt-shell/qml
conf=$HOME/.config/basalt/keyboard.conf
swayconf=$HOME/.config/basalt-shell/keyboard/sway.conf
pass=0
fail=0
keeper=""

swaysock() { find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'sway-ipc.*.sock' 2>/dev/null | head -1; }
ui() { qs -p "$qml" ipc --any-display call shell "$@" 2>/dev/null; }
start_keeper() {
  wtype -k XF86WakeUp -s 900000 -k XF86WakeUp &
  keeper=$!
  sleep 1
}
trap '[ -n "$keeper" ] && kill $keeper 2>/dev/null' EXIT
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
type_text() { wtype "$1"; sleep 0.5; }
shot() { sleep 0.4; grim "/out/kbd-$1.png"; }
# expect PATTERN [what]: the focused control matches, within 5 seconds
# (a change goes to the daemon and back before the focus moves; a session
# with many windows on the software renderer takes longer).
expect() {
  local got i
  for i in $(seq 1 20); do
    got=$(ui focused)
    # shellcheck disable=SC2053 # pattern on purpose
    [[ $got == $1 ]] && break
    sleep 0.25
  done
  # shellcheck disable=SC2053
  if [[ $got == $1 ]]; then pass=$((pass + 1)); echo "ok    ${2:-focus} = $got"
  else fail=$((fail + 1)); echo "FAIL  ${2:-focus}: want $1, got '$got' (surfaces: $(ui surfaces))"; fi
}
# check COMMAND what: true within 5 seconds.
check() {
  local i
  for i in $(seq 1 20); do
    if eval "$1"; then pass=$((pass + 1)); echo "ok    $2"; return; fi
    sleep 0.25
  done
  fail=$((fail + 1)); echo "FAIL  $2"
}
has() { grep -qF -- "$2" "$1" 2>/dev/null; }

export SWAYSOCK
SWAYSOCK=$(swaysock)
start_keeper

echo "== Settings, Keyboard: the page"
k logo+comma; sleep 1
k k; sleep 0.8
expect settings-nav-keyboard "type-ahead k: Keyboard"
check '[[ $(ui surfaces) == *"page:keyboard"* ]]' "the Keyboard page shows"
shot 01-page
k Tab
expect keyboard-add "Tab into the page: Add a layout (one layout: no row buttons)"

echo "== Add Portuguese (Brazil, ABNT2) by searching its name"
k Return; sleep 0.6
expect keyboard-search "Add a layout opens the search, focused"
type_text "abnt2"
shot 02-picker-abnt2
k Return; sleep 1
expect keyboard-add "Return takes the first match and goes back to Add a layout"
check 'has "$conf" "layouts = us, br"' "keyboard.conf: layouts = us, br"
check 'has "$swayconf" "xkb_layout \"us,br\""' "sway's start-up file names us,br"

echo "== Add English (US, international with dead keys) by its code, Down, Return"
k Return; sleep 0.6
type_text "us(intl)"
k Down
expect keyboard-pick-0 "Down from the search: the first match"
shot 03-picker-intl
k Return; sleep 1
check 'has "$conf" "layouts = us, br, us(intl)"' "keyboard.conf: us, br, us(intl)"
check 'has "$swayconf" "xkb_variant \",,intl\""' "sway's start-up file: variants ,,intl"

echo "== Escape closes the picker, not the page"
k Return; sleep 0.6
k Escape; sleep 0.5
expect keyboard-add "Escape in the search returns to Add a layout"

echo "== Reorder: Brazilian first"
k shift+Tab
expect keyboard-up-2 "Shift+Tab: the last row's buttons (one stop)"
k shift+Tab
expect keyboard-up-1 "Shift+Tab: the second row"
k Return; sleep 1
expect keyboard-down-0 "Moved to the top: the focus stays on its row"
check 'has "$conf" "layouts = br, us, us(intl)"' "keyboard.conf: br, us, us(intl)"
shot 04-reordered

echo "== Remove the plain US layout"
k Tab
expect keyboard-up-1 "Tab: the second row"
k Right Right
expect keyboard-remove-1 "Right, Right: Remove"
k Return; sleep 1
check 'has "$conf" "layouts = br, us(intl)"' "keyboard.conf: br, us(intl)"
expect "keyboard-*" "the focus stays on the page"
shot 05-removed

echo "== The switch key and Caps Lock"
for _ in 1 2 3 4 5 6; do
  [[ $(ui focused) == keyboard-switch-* ]] && break
  k Tab
done
expect "keyboard-switch-super+shift+space" "Tab reaches the switch keys on the selected one"
k Right Return; sleep 1
check 'has "$conf" "switch = alt+shift"' "keyboard.conf: switch = alt+shift"
check 'has "$swayconf" "grp:alt_shift_toggle"' "sway's start-up file: grp:alt_shift_toggle"
k Tab
expect keyboard-try "Tab: the try field"
type_text "ação, pé, çedilha"
shot 06-try
k Tab
expect "keyboard-caps-normal" "Tab: Caps Lock, on the selected one"
k Right Return; sleep 1
check 'has "$conf" "caps_lock = ctrl"' "keyboard.conf: caps_lock = ctrl"
check 'has "$swayconf" "grp:alt_shift_toggle,ctrl:nocaps"' "sway's start-up file: both options"
shot 07-keys
ind=$(basalt-shell ctl keyboard.indicator 2>/dev/null | tr -d ' \n')
check '[[ $ind == *"\"labels\":[\"BR\",\"US\"]"* ]]' "the panel's indicator: BR, US ($ind)"
check 'basalt-shell ctl activity "{\"n\":30}" 2>/dev/null | grep -q "keyboard settings"' "the changes are in the activity log"
check '! basalt-shell ctl activity "{\"n\":30}" 2>/dev/null | grep -q "keyboard settings saved, not applied"' "sway took every change"
k ctrl+w; sleep 0.5

echo "== A new session: the settings come back"
kill "$keeper" 2>/dev/null
keeper=""
# No procps in the image: ask sway to exit, then watch /proc.
running() { grep -qlx "$1" /proc/[0-9]*/comm 2>/dev/null; }
swaymsg exit >/dev/null 2>&1
for _ in $(seq 1 60); do running sway || break; sleep 0.25; done
for _ in $(seq 1 60); do running basalt-shelld || break; sleep 0.25; done
# A stand-in for the system assistant's command line: it stores nothing
# and logs what the shell asked for.
mkdir -p /tmp/fakebin
cat >/tmp/fakebin/basalt <<'EOF'
#!/bin/sh
echo "$*" >>/tmp/fake-basalt.log
case "$1" in
  keyboard) echo '{"id":"p-0c0d0e","stored":true}' ;;
  show)
    if [ "${3:-}" = --json ]; then
      echo '{"id":"p-0c0d0e","title":"use br,us(intl) for the login screen, the console and new accounts","kind":"keyboard","status":"pending"}'
    else
      echo "Proposal p-0c0d0e: use br,us(intl) for the login screen, the console and new accounts"
      echo "  1. localectl set-x11-keymap --no-convert br,us pc105 ,intl grp:alt_shift_toggle,ctrl:nocaps"
      echo "  2. localectl set-keymap --no-convert br"
      echo "  (without a prompt: sudo basalt apply p-0c0d0e --yes --confirm 0a0b0c0d)"
    fi ;;
  pending) echo '[]' ;;
  updates) echo '{"updates":[],"counts":{},"restart":{"needed":false},"history":[]}' ;;
  channels)
    # basalt-nonfree-testing is not defined here and nothing offers
    # basalt-nonfree-release (no drivers report in the container).
    echo '{"channels":[{"id":"basalt","defined":true,"enabled":true,"toggle":false,"signature":{"gpgcheck":true,"openbasalt":true,"short":"0A0B0C0D"}},'\
'{"id":"basalt-testing","defined":true,"enabled":false,"toggle":true,"testing":true,"signature":{"gpgcheck":true,"openbasalt":true,"short":"0A0B0C0D"}},'\
'{"id":"basalt-nonfree-testing","defined":false,"enabled":false,"toggle":true,"testing":true,"nonfree":true,"signature":{}}],'\
'"sources":[],"other":[],"catalog":[],"nonfree_defined":false,"nonfree_testing_defined":false,"docs":"https://example.org"}' ;;
  *) echo '{}' ;;
esac
EOF
chmod +x /tmp/fakebin/basalt
rm -f "$XDG_RUNTIME_DIR"/wayland-1 "$XDG_RUNTIME_DIR"/wayland-1.lock
PATH=/tmp/fakebin:$PATH nohup dbus-run-session -- basalt-session headless >/tmp/session2.log 2>&1 &
up=no
for _ in $(seq 1 120); do
  if [ -S "$XDG_RUNTIME_DIR/wayland-1" ] && ui surfaces >/dev/null 2>&1 && basalt-shell ctl keyboard.indicator >/dev/null 2>&1; then up=yes; break; fi
  sleep 0.5
done
check '[ "$up" = yes ]' "the new session came up"
SWAYSOCK=$(swaysock)
sleep 1
start_keeper
# sway 1.11 does not list included files (get_config) and has no real
# keyboard here: the start-up file is there and the shipped config
# includes it before the person's own additions.
check 'has "$swayconf" "xkb_layout \"br,us\"" && grep -qx "include ~/.config/basalt-shell/keyboard/\*.conf" "$BASALT_SHELL_DATA/sway/config"' "the next session's sway reads the person's layouts (start-up file included)"
ind=$(basalt-shell ctl keyboard.indicator 2>/dev/null | tr -d ' \n')
check '[[ $ind == *"\"labels\":[\"BR\",\"US\"]"* ]]' "the indicator after the restart: BR, US"
state=$(basalt-shell ctl keyboard.state 2>/dev/null | tr -d ' \n')
check '[[ $state == *"\"own\":true"* && $state == *"\"switch\":\"alt+shift\""* && $state == *"\"caps_lock\":\"ctrl\""* ]]' "keyboard.state after the restart: own layouts, alt+shift, ctrl"
shot 08-panel-indicator

echo "== Use my layouts there too: the proposal, on Not now"
k logo+comma; sleep 1
k k; sleep 0.8
expect settings-nav-keyboard "type-ahead k: Keyboard"
for _ in $(seq 1 20); do
  [[ $(ui focused) == keyboard-system ]] && break
  k Tab
done
expect keyboard-system "Tab reaches Use my layouts there too"
shot 09-system-button
k Return; sleep 2
expect keyboard-system-cancel "the sheet opens on Not now"
check 'grep -qx "keyboard set br,us(intl) --options grp:alt_shift_toggle,ctrl:nocaps --json" /tmp/fake-basalt.log' "the shell asked the assistant for keyboard set br,us(intl) --options grp:alt_shift_toggle,ctrl:nocaps --json"
shot 10-system-sheet
k shift+Tab
expect keyboard-system-confirm "Shift+Tab reaches the positive button"
k shift+Tab
expect keyboard-system-details "Shift+Tab: Details"
k Return; sleep 0.5
shot 11-system-details
k Escape; sleep 0.6
expect keyboard-system "Escape is Not now: back on the button"
check '! grep -q "apply" /tmp/fake-basalt.log' "nothing was applied"
check '! grep -rq localectl /tmp/session2.log' "the shell ran no localectl"

echo "== Updates and channels with the keyboard"
k Escape
expect settings-nav-keyboard "Escape from the page: the sidebar"
k u; sleep 2
expect settings-nav-updates "type-ahead u: Updates and channels"
k Tab
# shellcheck disable=SC2016 # check evaluates it
check '[[ $(ui focused) != settings-nav-* && -n $(ui focused) ]]' "Tab goes into the Updates page"
for _ in $(seq 1 30); do
  [[ $(ui focused) == channels-show-all ]] && break
  k Tab
done
expect channels-show-all "Tab reaches Show all channels"
k Return; sleep 1
seen=" "
for _ in $(seq 1 30); do
  k Tab
  seen="$seen$(ui focused) "
done
shot 12-updates-channels
check '[[ $seen == *" channel-basalt-testing "* ]]' "Tab reaches the basalt-testing toggle"
check '[[ $seen != *" channel-basalt-nonfree-testing "* ]]' "the unpublished basalt-nonfree-testing toggle is disabled (no Tab stop)"
check '[ "$(tr " " "\n" <<<"$seen" | sort -u | grep -c .)" -ge 4 ]' "Tab moves through the page's controls"
k Escape
expect settings-nav-updates "Escape returns to the sidebar"
check '! grep -q "channels enable basalt-nonfree-testing" /tmp/fake-basalt.log' "nothing asked to turn on basalt-nonfree-testing"
k ctrl+w; sleep 0.5

echo
echo "passed $pass, failed $fail"
[ "$fail" -eq 0 ]
