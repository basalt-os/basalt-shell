#!/bin/bash
# Voice and answers in Brazilian Portuguese on an English desktop, then
# English again, in the running session of the voice lab VM (session
# user). Each request is spoken into the lab microphone with lab-say
# (Super+V held), synthesized from text by Piper; Portuguese uses a pt_BR
# voice kept in the lab directory only (test input, never part of Basalt
# OS). Proposals and permissions are confirmed by clicking the primary
# button, as a person would. The person's settings file is written
# directly (the same file the Settings window writes); the previous one
# and the theme settings are put back at the end.
#   lang-session.sh OUT.jsonl
# ONLY=pt or ONLY=en runs one half; PT_MODEL picks the Portuguese speech
# model (default ggml-small-q5_1).
set -u
out=${1:?out.jsonl}
here=$(cd "$(dirname "$0")" && pwd)
XDG_RUNTIME_DIR=/run/user/$(id -u)
export XDG_RUNTIME_DIR WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-wayland-1}
export YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/run/ydotoold.socket}
piper_dir=${LAB_PIPER_DIR:-/opt/basalt-voice-lab/models/piper}
findbtn=${FINDBTN:-$here/../demo/findbtn.py}
log=$HOME/.local/state/basalt-shell/audit.jsonl
conf=$HOME/.config/basalt/voice-and-assistant.conf
settings=$HOME/.config/basalt-shell/settings.json
backup=$(mktemp -d)
cp -a "$settings" "$backup/settings.json" 2>/dev/null
cp -a "$conf" "$backup/prefs.conf" 2>/dev/null
restore() {
  basalt-shell-ui ipc call shell close >/dev/null 2>&1
  if [ -f "$backup/prefs.conf" ]; then cp -a "$backup/prefs.conf" "$conf"; else rm -f "$conf"; fi
  rm -rf "$backup"
}
trap restore EXIT

lines() { wc -l <"$log"; }
wait_for() { # wait_for N PATTERN [SECONDS]
  for _ in $(seq $((${3:-200} * 2))); do
    tail -n +$(($1 + 1)) "$log" | grep -q -- "$2" && return 0
    sleep 0.5
  done
  return 1
}
# The accent of the theme on screen (the primary button's color), from
# the daemon.
accent() {
  basalt-shell ctl theme | python3 -c 'import json, sys; print(json.load(sys.stdin)["tokens"]["color.accent"].lstrip("#")[:6])'
}
click_primary() {
  local tmp=/tmp/lang-find.png xy=""
  for _ in 1 2 3 4 5 6 7 8; do
    grim "$tmp"
    xy=$(python3 "$findbtn" "$tmp" --accent "$(accent)" 2>/dev/null) && break
    xy=""; sleep 1
  done
  [ -n "$xy" ] || { echo "no button" >&2; return 1; }
  ydotool mousemove -a -x ${xy% *} -y ${xy#* } >/dev/null; sleep 0.6
  ydotool click 0xC0 >/dev/null
  sleep 0.4; ydotool mousemove -a -x 1900 -y 1060 >/dev/null
}
prefs() { mkdir -p "$(dirname "$conf")"; printf '%s\n' "$@" >"$conf"; touch "$conf"; sleep 1; }

# say LABEL VOICE TEXT [confirm|allow]: speak, wait for the answer, confirm
# a proposal or a permission when asked; one JSON line per request.
say() {
  local label=$1 voice=$2 text=$3 act=${4:-}
  basalt-shell-ui ipc call shell close >/dev/null; sleep 1.2
  local n; n=$(lines)
  timeout 60 lab-say --text "$text" "$piper_dir/$voice.onnx" >/dev/null 2>&1
  wait_for "$n" '"type":"voice","actor":"voice"' 240 || echo "no voice record for: $text" >&2
  local applied=""
  if [ "$act" = allow ] && tail -n +$((n + 1)) "$log" | grep -q '"need_grant"'; then
    sleep 2; local m; m=$(lines); click_primary
    # The shell runs the request again once the permission is in force.
    wait_for "$m" '"type":"skill","actor":"skill"' 240
    wait_for "$m" '"answer"' 240
  fi
  if [ "$act" = confirm ] && tail -n +$((n + 1)) "$log" | grep '"type":"voice","actor":"voice"' | grep -q '"kind":"proposal"'; then
    sleep 1.5; local m; m=$(lines); click_primary
    wait_for "$m" '"type":"apply"' 30 && applied=yes || applied=no
  fi
  sleep 1
  tail -n +$((n + 1)) "$log" | python3 -c '
import json, sys
label, said, applied = sys.argv[1:4]
voice = skill = None
applies = []
for l in sys.stdin:
    try: r = json.loads(l)
    except Exception: continue
    d = r.get("data") or {}
    if r.get("type") == "voice" and r.get("actor") == "voice": voice = r
    if r.get("type") == "skill": skill = r
    if r.get("type") == "apply": applies.append(r.get("text"))
v = (voice or {}).get("data") or {}
s = (skill or {}).get("data") or {}
print(json.dumps({"label": label, "said": said, "heard": (voice or {}).get("text"), "stt_language": v.get("stt_language"),
    "answer_language": v.get("answer_language"), "stt_model": v.get("stt_model"), "kind": v.get("kind"), "applied": applied or None,
    "applies": applies, "answer": s.get("answer"), "speech": s.get("speech"), "model": s.get("model"), "spoken": v.get("spoken"),
    "timing": v.get("timing"), "skill_timing": s.get("timing")}, ensure_ascii=False))' "$label" "$text" "$applied" | tee -a "$out"
  sleep 4
}

: >"$out"
# No text field may have focus (the words would be dictation): the
# Settings window, if open, is closed (opened first so the toggle closes).
basalt-shell-ui ipc call shell settingsPage voice >/dev/null; sleep 1
basalt-shell-ui ipc call shell toggle settings >/dev/null; sleep 1
# 1. Brazilian Portuguese on the English desktop (session LANG=en_US).
if [ "${ONLY:-pt}" = pt ] || [ -z "${ONLY:-}" ]; then
prefs "[speech]" "language = pt-BR" "model = ${PT_MODEL:-ggml-small-q5_1}"
say pt-darker pt_BR-faber-medium "Deixe mais escuro." confirm
say pt-theme pt_BR-faber-medium "Use o tema lichen." confirm
say pt-mail pt_BR-faber-medium "O que a Ana disse no último e-mail?" allow
say pt-files pt_BR-faber-medium "Encontre o PDF que o banco mandou no mês passado." allow
fi
# 2. English again: the system's defaults (ggml-base.en, English answers).
if [ "${ONLY:-en}" = en ] || [ -z "${ONLY:-}" ]; then
rm -f "$conf"; sleep 1
say en-darker en_US-kristin-medium "Make it darker." confirm
say en-theme en_US-kristin-medium "Use the basalt theme." confirm
say en-mail en_US-kristin-medium "What did Ana say in her last email?" allow
fi
# The theme the lab had before.
cp -a "$backup/settings.json" "$settings" 2>/dev/null
