#!/bin/bash
# End-to-end test of the login screen without PAM or a display: the real
# greeter UI (Quickshell, this tree's greeter/) in a headless sway, talking
# to the fake greetd (internal/greetd.Fake, greetd 0.10's answers), typed
# into with wtype, screenshots with grim. Needs sway with the pixman
# renderer (Fedora's sway; SwayFX draws with GLES only and needs a GPU),
# Quickshell, wtype, grim, and Go or a built fake (FAKE_GREETD=PATH, from
# `go build ./lab/greeter/fake-greetd`). As any user, in a lab VM:
#
#   FAKE_GREETD=/tmp/fake-greetd lab/greeter/e2e-fake.sh /tmp/greeter-e2e
#
# Checks: a wrong password is refused and says so, nothing is started; the
# right one starts the remembered session with the session environment; a
# second person gets their own remembered session (niri); the greeter
# reports itself drawn (ready file) and ends with status 0; no answer ever
# reaches a log.
set -eu
src=$(cd "$(dirname "$0")/../.." && pwd)
out=${1:-/tmp/greeter-e2e}
rm -rf "$out"; mkdir -p "$out"
t=$(mktemp -d)
export XDG_RUNTIME_DIR=$t/xdg; mkdir -m 700 "$XDG_RUNTIME_DIR"
fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok: $*"; }

# A tiny system: two people, two Basalt sessions, a configuration.
root=$t/root
mkdir -p "$root/etc/basalt" "$root/usr/share/wayland-sessions"
cat >"$root/etc/passwd" <<'EOF'
root:x:0:0:root:/root:/bin/bash
basalt:x:1000:1000:Basalt desktop:/home/basalt:/bin/bash
ana:x:1001:1001:Ana Souza:/home/ana:/bin/bash
EOF
printf 'UID_MIN 1000\nUID_MAX 60000\n' >"$root/etc/login.defs"
printf 'LANG=en_US.UTF-8\n' >"$root/etc/locale.conf"
printf 'NAME="Basalt OS"\n' >"$root/etc/os-release"
cp "$src/config/greeter/greeter.conf" "$root/etc/basalt/greeter.conf"
cp "$src/config/sessions/basalt-sway.desktop" "$src/config/sessions/basalt-niri.desktop" "$root/usr/share/wayland-sessions/"

# The installed layout of the greeter and of the shell's themes.
data=$t/data; shell=$t/shell
mkdir -p "$data/qml" "$data/locale" "$shell/qml"
cp "$src"/greeter/*.qml "$src"/greeter/logic.js "$data/qml/"
cp "$src"/greeter/locale/*.json "$data/locale/"
ln -s "$src/themes" "$shell/themes"
ln -s "$src/shell/wallpapers" "$shell/qml/wallpapers"

# The fake greetd.
if [ -n "${FAKE_GREETD:-}" ]; then cp "$FAKE_GREETD" "$t/fake-greetd"
else (cd "$src" && go build -o "$t/fake-greetd" ./lab/greeter/fake-greetd); fi
pw_basalt=staple$RANDOM; pw_ana=horse$RANDOM
"$t/fake-greetd" -sock "$t/greetd.sock" -user "basalt=$pw_basalt" -user "ana=$pw_ana" >"$out/fake-greetd.log" 2>&1 &
fake=$!
trap 'kill $fake 2>/dev/null; rm -rf "$t"' EXIT

export BASALT_GREETER_ROOT=$root BASALT_GREETER_DATA=$data BASALT_GREETER_SHELL_DATA=$shell
export BASALT_GREETER_STATE=$t/state BASALT_GREETER_RUN=$t/run GREETD_SOCK=$t/greetd.sock
mkdir -p "$BASALT_GREETER_STATE" "$BASALT_GREETER_RUN"
export WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1 WLR_HEADLESS_OUTPUTS=1
export QT_QUICK_BACKEND=software QT_QPA_PLATFORM=wayland QT_WAYLAND_DISABLE_WINDOWDECORATION=1 QV4_FORCE_INTERPRETER=1

# run_greeter NAME STATE_JSON SCRIPT: one greeter run (sway exits when the
# greeter does), SCRIPT runs once the screen is up.
run_greeter() {
  local name=$1 state=$2 script=$3
  printf '%s\n' "$state" >"$BASALT_GREETER_STATE/state.json"
  rm -f "$BASALT_GREETER_RUN"/ready "$BASALT_GREETER_RUN"/status
  cat >"$t/sway.conf" <<EOF
output HEADLESS-1 resolution 1920x1080 scale 1
exec sh -c 'quickshell -p "$data/qml" >"$out/$name-ui.log" 2>&1; echo \$? >"$BASALT_GREETER_RUN/status"; swaymsg exit'
EOF
  sway -c "$t/sway.conf" >"$out/$name-sway.log" 2>&1 &
  local sway=$!
  for _ in $(seq 60); do [ -e "$BASALT_GREETER_RUN/ready" ] && break; sleep 0.5; done
  [ -e "$BASALT_GREETER_RUN/ready" ] || fail "$name: the greeter never reported ready"
  export WAYLAND_DISPLAY SWAYSOCK
  for s in "$XDG_RUNTIME_DIR"/wayland-[0-9]; do WAYLAND_DISPLAY=${s##*/}; done
  for s in "$XDG_RUNTIME_DIR"/sway-ipc.*.sock; do SWAYSOCK=$s; done
  # A keyboard that stays: with no real input device, every wtype would
  # otherwise add and remove the seat's only keyboard, and the greeter's
  # window would lose the keyboard focus in between (a test artifact).
  wtype -k XF86WakeUp -s 600000 -k XF86WakeUp &
  local kb=$!
  sleep 1.5
  eval "$script"
  for _ in $(seq 30); do kill -0 $sway 2>/dev/null || break; sleep 0.5; done
  kill $sway $kb 2>/dev/null || true
  wait $sway 2>/dev/null || true
}
shot() { grim "$out/$1.png"; }

# 1. basalt (remembered): wrong password, then the right one.
run_greeter basalt '{"lastUser":"basalt","sessions":{"basalt":"basalt-sway.desktop"}}' '
  shot 01-start
  wtype "notit" -k Return; sleep 4.5
  shot 02-wrong
  grep -q "session started" "$out/fake-greetd.log" && fail "a session started after a wrong password"
  wtype "$pw_basalt" -k Return; sleep 3
'
grep -q 'session started: user=basalt cmd=\["basalt-session" "sway"\]' "$out/fake-greetd.log" || fail "basalt: no sway session started"
grep -q 'XDG_SESSION_DESKTOP=sway' "$out/fake-greetd.log" || fail "basalt: no session environment"
[ "$(cat "$BASALT_GREETER_RUN/status")" = 0 ] || fail "basalt: greeter status $(cat "$BASALT_GREETER_RUN/status")"
grep -q '"lastUser": "basalt"' "$BASALT_GREETER_STATE/state.json" || fail "basalt: not remembered"
pass "wrong password refused, right password started Basalt on Sway, greeter ended with 0"

# 2. ana, whose last session was niri.
run_greeter ana '{"lastUser":"ana","sessions":{"ana":"basalt-niri.desktop","basalt":"basalt-sway.desktop"}}' '
  shot 03-ana
  wtype "$pw_ana" -k Return; sleep 3
'
grep -q 'session started: user=ana cmd=\["basalt-session" "niri"\]' "$out/fake-greetd.log" || fail "ana: no niri session"
pass "ana's remembered session (Basalt on niri) started"

# 3. No answer reaches any log.
if grep -r -e "$pw_basalt" -e "$pw_ana" -e "notit" "$out" --include='*.log' -l; then fail "a password reached a log"; fi
if grep -r -e "$pw_basalt" -e "$pw_ana" "$BASALT_GREETER_STATE" -l; then fail "a password reached the state"; fi
pass "no password in the logs or the state"
echo "all greeter e2e checks passed; screenshots in $out"
