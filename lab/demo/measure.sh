#!/bin/bash
# Lab VM (desktop user, in the session): rough numbers for the prototype
# report. Frames delivered while the shell animates (wf-recorder only
# receives a frame when the screen changes, so frames per second during a
# known animation approximate what the compositor presents), idle CPU and
# memory of the compositor, the shell UI and the daemon, and IPC latency.
#   measure.sh OUTFILE
set -u
outf=${1:?output file}
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export WAYLAND_DISPLAY=$(ls "$XDG_RUNTIME_DIR" | grep -E '^wayland-[0-9]+$' | head -1)
export SWAYSOCK=$(ls -t "$XDG_RUNTIME_DIR"/sway-ipc.*.sock 2>/dev/null | head -1)
export NIRI_SOCKET=$(ls -t "$XDG_RUNTIME_DIR"/niri.*.sock 2>/dev/null | head -1)
comp=niri; [ -n "$SWAYSOCK" ] && pgrep -x sway >/dev/null && comp=sway && unset NIRI_SOCKET
ui() { basalt-shell-ui ipc call shell "$@" >/dev/null; }
{
  echo "compositor: $comp ($(basalt-shell ctl desktop | jq -r .version))"
  echo "renderer: $(cat /sys/class/drm/renderD128/device/virtio*/features 2>/dev/null | cut -c1 | sed 's/1/virgl (host GPU)/;s/0/2D, llvmpipe/')"
  echo "motion: $(basalt-shell ctl theme | jq -r '.tokens.motion')"
  # Animation: open and close the side drawer (motion.slow) and the launcher.
  rm -f /tmp/anim.webm
  wf-recorder -y -c libvpx -p deadline=realtime -p cpu-used=8 -f /tmp/anim.webm >/dev/null 2>&1 & r=$!
  sleep 1
  for _ in 1 2 3; do ui open activity; sleep 0.9; ui close; sleep 0.9; ui toggle launcher; sleep 0.7; ui close; sleep 0.7; done
  kill -INT $r; wait $r 2>/dev/null
  frames=$(ffprobe -v error -count_frames -select_streams v:0 -show_entries stream=nb_read_frames -of csv=p=0 /tmp/anim.webm)
  dur=$(ffprobe -v error -show_entries format=duration -of csv=p=0 /tmp/anim.webm)
  echo "animation capture: $frames frames in ${dur}s (frames arrive only on change; 3 x drawer + launcher open/close)"
  # Idle: 10 s of CPU time.
  pids="$(pgrep -x $comp | head -1) $(pgrep -x qs | head -1) $(pgrep -f '^basalt-shell daemon' | head -1)"
  t0=$(for p in $pids; do awk '{print $14+$15}' /proc/$p/stat; done | paste -sd' ')
  sleep 10
  t1=$(for p in $pids; do awk '{print $14+$15}' /proc/$p/stat; done | paste -sd' ')
  hz=$(getconf CLK_TCK)
  parr=($pids); a=($t0); b=($t1); i=0
  for name in "$comp" "quickshell" "basalt-shell daemon"; do
    p=${parr[$i]}
    rss=$(awk '/VmRSS/ {print $2}' /proc/$p/status)
    cpu=$(awk -v d=$(( ${b[$i]} - ${a[$i]} )) -v hz=$hz 'BEGIN { printf "%.1f", d / hz / 10 * 100 }')
    echo "idle $name: cpu ${cpu}% (10 s), rss $((rss / 1024)) MiB"
    i=$((i + 1))
  done
  # IPC latency: 20 desktop reads through the daemon.
  s=$(date +%s%N); for _ in $(seq 20); do basalt-shell ctl desktop >/dev/null; done; e=$(date +%s%N)
  echo "ctl desktop round trip (incl. process start): $(( (e - s) / 20000000 )) ms average"
} | tee "$outf"
