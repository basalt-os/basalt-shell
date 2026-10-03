#!/bin/bash
# Lab VM (desktop user): settings that only make scripted input reliable.
# Not part of Basalt OS.
set -eu
conf=${XDG_CONFIG_HOME:-$HOME/.config}/basalt-shell
# sway: 1:1 pointer for ydotool's absolute moves.
mkdir -p "$conf/sway.d"
printf 'input type:pointer accel_profile flat\n' >"$conf/sway.d/20-lab-input.conf"
# niri: the same, and no hot corner (ydotool moves through 0,0).
mkdir -p "$conf/niri"
cat >"$conf/niri/local.kdl" <<'KDL'
// lab only: 1:1 pointer for scripted clicks (ydotool), no hot corner
input {
    mouse {
        accel-profile "flat"
    }
}
gestures {
    hot-corners {
        off
    }
}
KDL
