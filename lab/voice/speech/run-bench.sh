#!/bin/bash
# The speech-to-text benchmark matrix on the VM (CPU only). Results in
# /opt/basalt-voice-lab/bench/results/*.jsonl and summary.jsonl.
set -u
L=/opt/basalt-voice-lab
S=$L/bench/set
R=$L/bench/results
mkdir -p $R
P=$L/venv/bin/python
B=$L/bench/speech/bench-stt.py
W=$L/models/whisper
run() { name=$1; shift; echo "== $name $(cat /proc/loadavg)" >&2; $P $B $S $R/$name.jsonl "$@" | tee -a $R/summary.jsonl; }
run cli-tiny.en       cli $W/ggml-tiny.en.bin --every 3
run cli-base.en       cli $W/ggml-base.en.bin --every 3
run cli-base.en-q5_1  cli $W/ggml-base.en-q5_1.bin --every 3
run cli-base.en-ac768 cli $W/ggml-base.en.bin --every 3 --ac 768
run server-base.en    server $W/ggml-base.en.bin --every 3
run fw-tiny.en        fw $L/models/fw/tiny.en --every 3
run fw-base.en        fw $L/models/fw/base.en --every 3
run fw-distil-small.en fw $L/models/fw/distil-small.en --every 3
run cli-small.en      cli $W/ggml-small.en.bin --every 6
run cli-small.en-q5_1 cli $W/ggml-small.en-q5_1.bin --every 6
run fw-small.en       fw $L/models/fw/small.en --every 6
run cli-turbo-q5_0    cli $W/ggml-large-v3-turbo-q5_0.bin --every 11
echo done >&2
