#!/bin/bash
# End-to-end voice timings in the running session (lab VM, session user):
# each request is spoken into the lab microphone (lab-say holds Super+V),
# and the shell's own "voice" record gives release-to-text,
# release-to-answer and release-to-first-audio. A sampler records peak
# memory and CPU of the processes involved. The permissions must be in
# place (the demo or the person allowed them).
#   e2e-voice.sh OUT.jsonl
set -u
out=${1:?out.jsonl}
export XDG_RUNTIME_DIR=/run/user/$(id -u) WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-wayland-1}
log=$HOME/.local/state/basalt-shell/audit.jsonl
samp=$(mktemp)
( while true; do ps -eo rss=,pcpu=,comm= | awk -v t="$(date +%s.%N)" '$3 ~ /basalt-voiced|whisper-cli|piper|llama-server|chrom|basalt-skill|basalt-shelld|quickshell|pw-record/ {print t, $1, $2, $3}'; sleep 0.5; done ) > "$samp" &
SP=$!
: > "$out"
while IFS= read -r req; do
  [ -z "$req" ] && continue
  basalt-shell-ui ipc call shell close >/dev/null; sleep 1.2
  s=$(grep -c . "$log")
  timeout 40 lab-say --text "$req"
  for _ in $(seq 240); do tail -n +$((s+1)) "$log" | grep -q '"type":"voice","actor":"voice"' && break; sleep 0.5; done
  tail -n +$((s+1)) "$log" | grep '"type":"voice","actor":"voice"' | tail -1 | python3 -c '
import json,sys
r=json.loads(sys.stdin.read() or "{}"); d=r.get("data",{})
print(json.dumps({"request": sys.argv[1], "heard": r.get("text"), "kind": d.get("kind"), "timing": d.get("timing"), "spoken": d.get("spoken")}))' "$req" | tee -a "$out"
  sleep 6   # let the answer finish speaking
done <<'REQ'
Make the text bigger.
Find the PDF the bank sent last month.
Where is my passport scan?
Find the spreadsheet with the travel budget.
What did Ana say in her last email?
Do I have any email from the landlord about the rent?
Summarize the lab news page.
Summarize the email about the invoice that is overdue.
REQ
kill $SP
python3 - "$samp" <<'PY' | tee -a "$out"
import sys, collections
peak = collections.defaultdict(int); cpu = collections.defaultdict(float)
for l in open(sys.argv[1]):
    p = l.split()
    if len(p) < 4: continue
    peak[p[3]] = max(peak[p[3]], int(p[1])); cpu[p[3]] = max(cpu[p[3]], float(p[2]))
import json
print(json.dumps({"peak_rss_mib": {k: round(v/1024) for k, v in peak.items()}, "peak_cpu_pct": cpu}))
PY
rm -f "$samp"
