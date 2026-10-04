#!/bin/bash
# Command bar with the local translator (lab VM, session user): types each
# request into the bar (Quickshell IPC, which submits it), waits for the answer and prints how it was understood (from
# the activity log), then closes the bar.
#   commandbar-test.sh [OUTDIR]
set -u
out=${1:-/tmp/commandbar}
mkdir -p "$out"
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/$(id -u)} YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/run/ydotoold.socket}
export WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-$(ls "$XDG_RUNTIME_DIR" | grep -E '^wayland-[0-9]+$' | head -1)}
log=${XDG_STATE_HOME:-$HOME/.local/state}/basalt-shell/audit.jsonl
key() { ydotool key "$1:1" "$1:0" >/dev/null; }
i=0
while IFS= read -r req; do
  [ -z "$req" ] && continue
  i=$((i+1))
  before=$(wc -l <"$log")
  basalt-shell-ui ipc call shell ask "$req" >/dev/null
  # (the bar submits a request given through IPC by itself)
  for _ in $(seq 120); do
    [ "$(tail -n +$((before+1)) "$log" | grep -c '"type":"ask"')" -ge 1 ] && break
    sleep 0.25
  done
  sleep 2.5           # system questions: the assistant's report follows the ask record
  grim "$out/$(printf %02d $i).png"
  tail -n +$((before+1)) "$log" | jq -c --arg r "$req" 'select(.type=="ask") | {request: $r, backend: .data.backend,
     ms: .data.model.elapsed_ms, via: .data.model.via, model_answer: .data.model.answer, calls: [.data.calls[]?.action],
     system: .data.system, ask_system: .data.ask_system, clarify: .data.clarify}'
  key 1               # Escape closes the bar
  sleep 0.6
done <<'REQ'
make it darker with rounder corners
deixa o tema claro e a cor de destaque verde
por favor, coloca as janelas uma do lado da outra
abre o editor de texto
manda o foot pra área de trabalho dois
I can't read anything, the letters are tiny
switch to the tide look
por que o nginx caiu?
quanto espaço sobra no disco?
instala o steam
REQ
