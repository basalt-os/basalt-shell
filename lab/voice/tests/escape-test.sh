#!/bin/bash
# Escape matrix of the voice and skill domains (lab VM, enforcing). As
# root: copies the probe with each domain's program type; then, as the
# session user, runs it in that domain (the worker's copy inside a
# session registered with basalt-resolver with an allowlist of
# news.lab.test only, as the shell does for a web job).
#   escape-test.sh [OUT_DIR]
set -euo pipefail
out=${1:-/home/basalt/voice-lab/results}
mkdir -p "$out"
probe=/home/basalt/voice-lab/tests/probe/probe
install -d /usr/local/libexec/lab-probe
for t in basalt_skill_exec_t basalt_skill_index_exec_t basalt_voice_exec_t; do
  install -m 0755 "$probe" /usr/local/libexec/lab-probe/probe-$t
  chcon -t $t /usr/local/libexec/lab-probe/probe-$t
done
chown basalt:basalt "$out"
run_user() { (cd /tmp && runuser -u basalt -- env XDG_RUNTIME_DIR=/run/user/1000 HOME=/home/basalt DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus "$@"); }
for t in basalt_skill_index_exec_t basalt_voice_exec_t; do
  # Through a pipe: the confined probe may not write a file in the home
  # folder, not even its own stdout redirected there.
  run_user /usr/local/libexec/lab-probe/probe-$t 2>/dev/null | cat > "$out/escape-$t.jsonl" || true
done
# The worker's domain, in a resolver session (default deny, news.lab.test only).
run_user python3 /home/basalt/voice-lab/tests/in-session.py news.lab.test /usr/local/libexec/lab-probe/probe-basalt_skill_exec_t 2>/dev/null | cat > "$out/escape-basalt_skill_exec_t.jsonl" || true
for f in "$out"/escape-*.jsonl; do
  echo "== $(basename "$f")"
  python3 - "$f" <<'PY'
import json, sys
for l in open(sys.argv[1]):
    try:
        r = json.loads(l)
    except ValueError:
        print("   ", l.strip()); continue
    print("  %-48s %s  %s" % (r["attempt"], "ALLOWED" if r["allowed"] else "denied ", r["detail"][:90]))
PY
done
