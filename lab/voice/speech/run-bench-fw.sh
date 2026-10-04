#!/bin/bash
# faster-whisper part of the speech-to-text matrix (rerun after the
# decoder fix); smaller samples for the small models.
set -u
L=/opt/basalt-voice-lab; S=$L/bench/set; R=$L/bench/results; P=$L/venv/bin/python; B=$L/bench/speech/bench-stt.py
run() { name=$1; shift; echo "== $name $(cat /proc/loadavg)" >&2; $P $B $S $R/$name.jsonl "$@" | tee -a $R/summary.jsonl; }
run fw-tiny.en fw $L/models/fw/tiny.en --every 3
run fw-base.en fw $L/models/fw/base.en --every 3
run fw-distil-small.en fw $L/models/fw/distil-small.en --every 6
run fw-small.en fw $L/models/fw/small.en --every 6
echo done >&2
