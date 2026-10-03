#!/bin/bash
# Development loop without a display: run a compositor headless inside the
# dev container (scripts/dev/Containerfile), start the shell, take
# screenshots with grim. Usage (inside the container, repo at /src):
#   scripts/dev/headless.sh sway|niri OUTDIR [COMMANDS_FILE]
# COMMANDS_FILE: shell lines run after the shell started (one per line;
# "shot NAME" takes a screenshot).
set -eu
which=${1:-sway}; out=${2:-/tmp/shots}; cmds=${3:-}
mkdir -p "$out"
mkdir -p "$XDG_RUNTIME_DIR" 2>/dev/null || export XDG_RUNTIME_DIR=/tmp/xdg-$(id -u)
mkdir -p "$XDG_RUNTIME_DIR"; chmod 700 "$XDG_RUNTIME_DIR"
stage=/tmp/stage
rm -rf "$stage"; [ -x /src/build/basalt-shell ] || make -C /src -s build >/dev/null
make -C /src -s install DESTDIR=$stage PREFIX=/usr SYSCONFDIR=/etc >/dev/null
export PATH=$stage/usr/bin:$PATH BASALT_SHELL_DATA=$stage/usr/share/basalt-shell
export QT_QUICK_BACKEND=${QT_QUICK_BACKEND:-software}
export WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1 WLR_HEADLESS_OUTPUTS=1
cat > /tmp/sway.d.conf <<EOC
output HEADLESS-1 resolution 1600x1000 scale 1
EOC
mkdir -p ~/.config/basalt-shell/sway.d; cp /tmp/sway.d.conf ~/.config/basalt-shell/sway.d/10-headless.conf
shot() { grim -o "${OUTPUT_NAME:-HEADLESS-1}" "$out/$1.png" 2>/dev/null || grim "$out/$1.png"; echo "shot $1"; }
export -f shot
run() {
  if [ "$which" = sway ]; then
    basalt-session sway > "$out/session.log" 2>&1 &
  else
    # niri has no headless backend: nest it in a headless sway.
    sway -c /dev/null > "$out/host.log" 2>&1 &
    sleep 2
    export WAYLAND_DISPLAY=wayland-1
    swaymsg -s "$(ls $XDG_RUNTIME_DIR/sway-ipc.*.sock | head -1)" output HEADLESS-1 resolution 1700x1100 >/dev/null || true
    basalt-session niri > "$out/session.log" 2>&1 &
  fi
}
dbus-run-session -- bash -c "$(declare -f run shot); export out=$out which=$which; run; sleep 6;
  export WAYLAND_DISPLAY=\$(ls \$XDG_RUNTIME_DIR | grep -E '^wayland-[0-9]+\$' | sort | tail -1)
  export SWAYSOCK=\$(ls \$XDG_RUNTIME_DIR/sway-ipc.*.sock 2>/dev/null | head -1)
  export NIRI_SOCKET=\$(ls \$XDG_RUNTIME_DIR/niri.*.sock 2>/dev/null | head -1)
  shot 00-start
  if [ -n '$cmds' ]; then while IFS= read -r line; do [ -z \"\$line\" ] && continue; eval \"\$line\"; done < '$cmds'; fi
  sleep 1"
