#!/bin/bash
# Zero-setup lab, step 3 (as root in the VM): feed.sh USER FILE.wav speaks
# FILE (16 kHz mono) into the lab microphone of USER's session once push to
# talk opened the capture. The key is held from the host through the QEMU
# monitor while this runs (sendkey meta_l-v 6000). Lab only.
set -eu
u=$1 wav=$2 uid=$(id -u "$1")
for _ in $(seq 80); do
  runuser -u "$u" -- env XDG_RUNTIME_DIR=/run/user/$uid pactl list short sources 2>/dev/null | grep -q 'lab_mic.*RUNNING' && break
  sleep 0.1
done
sleep 0.3
python3 - "$wav" "/run/user/$uid/lab-mic.fifo" <<'PY'
import sys, time, wave
w = wave.open(sys.argv[1]); pcm = w.readframes(w.getnframes())
data = b"\0" * 8000 + pcm + b"\0" * 6400
with open(sys.argv[2], "wb", buffering=0) as f:
    t0 = time.monotonic()
    for i in range(0, len(data), 3200):
        f.write(data[i:i + 3200])
        dt = t0 + (i + 3200) / 32000 - time.monotonic()
        if dt > 0: time.sleep(dt)
PY
echo fed
