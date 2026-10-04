#!/bin/bash
# Escape matrix of the voice and skill domains (lab VM, enforcing). As
# root: copies the probe with each domain's program type; then, as the
# session user, runs it in that domain (the reading worker inside a
# session registered with basalt-resolver allowing news.lab.test only, as
# the shell does for a web job; the sender in a session allowing the lab
# SMTP server only, as for a reply), and checks every attempt against
# what that domain may do (tests/escape-expect.py).
#   escape-test.sh [OUT_DIR]
set -euo pipefail
out=${1:-/home/basalt/voice-lab/results}
mkdir -p "$out"
probe=/home/basalt/voice-lab/tests/probe/probe
install -d /usr/local/libexec/lab-probe
types="basalt_skill_exec_t basalt_skill_index_exec_t basalt_voice_exec_t basalt_skill_send_exec_t basalt_skill_files_exec_t"
for t in $types; do
  install -m 0755 "$probe" /usr/local/libexec/lab-probe/probe-$t
  chcon -t $t /usr/local/libexec/lab-probe/probe-$t
done
chown basalt:basalt "$out"
# A file the mover may rename (and nobody may delete).
src=/home/basalt/Documents/probe-rename-src.txt
echo probe > $src && chown basalt:basalt $src && restorecon $src
run_user() { (cd /tmp && runuser -u basalt -- env XDG_RUNTIME_DIR=/run/user/1000 HOME=/home/basalt DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus "$@"); }
for t in basalt_skill_index_exec_t basalt_voice_exec_t basalt_skill_files_exec_t; do
  # Through a pipe: the confined probe may not write a file in the home
  # folder, not even its own stdout redirected there.
  run_user /usr/local/libexec/lab-probe/probe-$t 2>/dev/null | cat > "$out/escape-$t.jsonl" || true
done
# The reading worker's domain, in a resolver session (default deny, news.lab.test only).
run_user python3 /home/basalt/voice-lab/tests/in-session.py news.lab.test /usr/local/libexec/lab-probe/probe-basalt_skill_exec_t 2>/dev/null | cat > "$out/escape-basalt_skill_exec_t.jsonl" || true
# The sender's domain, in a session allowing the lab SMTP server only.
run_user python3 /home/basalt/voice-lab/tests/in-session.py "mail.example.com:587 private" /usr/local/libexec/lab-probe/probe-basalt_skill_send_exec_t 2>/dev/null | cat > "$out/escape-basalt_skill_send_exec_t.jsonl" || true
rm -f $src /home/basalt/Documents/probe-rename-dst.txt
python3 /home/basalt/voice-lab/tests/escape-expect.py "$out"
