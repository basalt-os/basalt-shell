#!/bin/bash
# Security test of the confirmation boundary (lab VM, as the session user,
# inside a running Basalt sway session with the basalt_shell policy).
#   security-test.sh            run the attacks, then let the "person"
#                               confirm through the real UI (ydotool)
# Root afterwards: ausearch -m avc -ts <printed start time>
set -u
here=$(dirname "$(readlink -f "$0")")
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}
export SWAYSOCK=${SWAYSOCK:-$(ls -t "$XDG_RUNTIME_DIR"/sway-ipc.*.sock | head -1)}
export WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-$(ls "$XDG_RUNTIME_DIR" | grep -E '^wayland-[0-9]+$' | head -1)}
echo "start: $(date '+%m/%d/%Y %H:%M:%S')"
pass=0; fail=0
check() { # NAME EXPECT(refused|ok) OUTPUT
  local got=ok
  if grep -q -i -E 'refused|reserved|denied|not the shell UI|only the shell UI|another shell UI|permission' <<<"$3"; then got=refused; fi
  if [ "$got" = "$2" ]; then pass=$((pass+1)); r=PASS; else fail=$((fail+1)); r=FAIL; fi
  printf '%-4s %-62s %s\n' "$r" "$1" "$(tr '\n' ' ' <<<"$3" | cut -c1-150)"
}

id=$(basalt-shell propose notification.show '{"summary": "security test: only the person may confirm this"}' --wait 0 2>&1 | jq -r .id)
echo "pending proposal from an agent: $id"
dec="{\"id\": \"$id\", \"approve\": true}"

out=$(python3 "$here/ipc.py" ui decide "$dec" 2>&1)
check "T1 unconfined_t process (python3) asks for ui and confirms" refused "$out"

mkdir -p /tmp/fake && cp /usr/bin/python3 /tmp/fake/quickshell
out=$(/tmp/fake/quickshell "$here/ipc.py" ui decide "$dec" 2>&1)
check "T2 program named quickshell, outside the UI domain" refused "$out"

out=$(BASALT_SHELL_ROLE=ui basalt-shell ctl decide "$dec" 2>&1)
check "T3 basalt_agent_mcp_t (basalt-shell ctl) confirms" refused "$out"

out=$(printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"proposal_status","arguments":{"id":"'"$id"'"}}}' | basalt-shell mcp 2>&1 | tail -1)
check "T4 MCP server has no confirm tool (proposal stays pending)" ok "$out"

out=$(/tmp/fake/quickshell "$here/ipc.py" --setcon basalt_shell_ui_t ui decide "$dec" 2>&1)
check "T5 unconfined code setcon to basalt_shell_ui_t while the UI runs" refused "$out"

out=$(BASALT_SHELL_SOCKET="$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY" basalt-shell ctl ping 2>&1)
check "T6 basalt_agent_mcp_t connects to the Wayland socket" refused "$out"
out=$(BASALT_SHELL_SOCKET="$XDG_RUNTIME_DIR/bus" basalt-shell ctl ping 2>&1)
check "T7 basalt_agent_mcp_t connects to the D-Bus session bus" refused "$out"
x11=$(ls /tmp/.X11-unix/X* 2>/dev/null | head -1)
if [ -n "$x11" ]; then
  out=$(BASALT_SHELL_SOCKET="$x11" basalt-shell ctl ping 2>&1)
  check "T8 basalt_agent_mcp_t connects to the X11 socket ($x11)" refused "$out"
fi
out=$(BASALT_SHELL_SOCKET="$XDG_RUNTIME_DIR/pipewire-0" basalt-shell ctl ping 2>&1)
check "T9 basalt_agent_mcp_t connects to PipeWire" refused "$out"

st=$(basalt-shell ctl proposal "{\"id\": \"$id\"}" | jq -r .status)
check "T10 after all attempts the proposal is still pending ($st)" ok "$([ "$st" = pending ] && echo ok || echo refused)"

# The person confirms on the sheet (Enter on the focused Confirm button,
# from a kernel-level virtual keyboard standing in for a real one).
# BASALT_TEST_CONFIRM replaces ydotool (for example a key sent by the VM's
# host to its virtual keyboard).
confirm=${BASALT_TEST_CONFIRM:-"env YDOTOOL_SOCKET=${YDOTOOL_SOCKET:-/run/ydotoold.socket} ydotool key 28:1 28:0"}
sleep 1.5
$confirm >/dev/null 2>&1
sleep 1.5
st=$(basalt-shell ctl proposal "{\"id\": \"$id\"}" | jq -r '.status + " by " + .decided_by')
check "T11 the person confirms in the shell UI ($st)" ok "$([[ $st == applied* ]] && echo ok || echo refused)"

# A second, genuine shell UI (the launcher is the domain's entry point)
# while the first one runs: refused, the running UI keeps the role.
timeout 6 /usr/libexec/basalt-shell/basalt-shell-ui-launch >/dev/null 2>&1
out=$(tail -n 5 "${XDG_STATE_HOME:-$HOME/.local/state}/basalt-shell/audit.jsonl" | grep -o 'another shell UI[^"]*' | tail -1)
check "T5b a second shell UI in basalt_shell_ui_t while the first runs" refused "${out:-accepted}"

# Applications the shell starts run in the person's domain (the domain of
# this script, the session's), never in the daemon's basalt_shell_t.
mine=$(id -Z | cut -d: -f3)
app=${BASALT_TEST_APP:-foot}
before=$(systemctl --user list-units --plain --no-legend 'app-basalt-*' | awk '{print $1}' | sort)
id=$(basalt-shell propose app.launch "{\"app\": \"$app\"}" --wait 0 2>&1 | jq -r .id)
sleep 1.5
$confirm >/dev/null 2>&1
sleep 3
unit=$(comm -13 <(echo "$before") <(systemctl --user list-units --plain --no-legend 'app-basalt-*' | awk '{print $1}' | sort) | head -1)
pid=$(systemctl --user show -p MainPID --value "$unit" 2>/dev/null)
dom=$(ps -o label= -p "${pid:-0}" 2>/dev/null | cut -d: -f3)
check "T12 a launched app ($app, $unit) runs in the session's domain ($dom)" ok "$([ -n "$dom" ] && [ "$dom" = "$mine" ] && echo ok || echo refused)"
others=$(ps -eo label=,comm= | awk '$1 ~ /:basalt_shell_t:/ && $2 != "basalt-shelld" {print $2}' | sort -u | tr '\n' ' ')
check "T13 nothing but basalt-shelld runs in basalt_shell_t (${others:-none})" ok "$([ -z "$others" ] && echo ok || echo refused)"
[ -n "$unit" ] && systemctl --user stop "$unit"

echo "audit (last records of this test):"
tail -n 14 "${XDG_STATE_HOME:-$HOME/.local/state}/basalt-shell/audit.jsonl" | jq -r '"  " + .type + "  " + .actor + "  " + .text' | cut -c1-160
basalt-shell audit verify
echo "result: $pass passed, $fail failed"
