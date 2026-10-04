#!/usr/bin/env python3
"""Raw IPC client for the shell daemon (lab tests).

    ipc.py ROLE [OP JSON]...   say hello with ROLE (ui or agent), then send each OP

Prints one JSON line per reply. With --setcon TYPE it first switches its own
SELinux domain (dynamic transition, allowed to unconfined code on Fedora),
to show what unconfined code can and cannot do.
"""
import json, os, socket, sys

args = sys.argv[1:]
if args and args[0] == "--setcon":
    import selinux  # python3-libselinux
    cur = selinux.getcon()[1].split(":")
    cur[2] = args[1]
    rc = selinux.setcon(":".join(cur))
    print(json.dumps({"setcon": ":".join(cur), "rc": rc}))
    args = args[2:]
role = args[0]
path = os.environ.get("BASALT_SHELL_SOCKET") or os.path.join(os.environ["XDG_RUNTIME_DIR"], "basalt-shell", "shell.sock")
s = socket.socket(socket.AF_UNIX)
s.connect(path)
f = s.makefile("rw")
n = 0

def call(op, a):
    global n
    n += 1
    f.write(json.dumps({"id": n, "op": op, "args": a}) + "\n")
    f.flush()
    while True:
        m = json.loads(f.readline())
        if m.get("id") == n:
            return m

print(json.dumps({"op": "hello", "reply": call("hello", {"role": role, "client": "lab-test"})}))
ops = args[1:]
for i in range(0, len(ops), 2):
    r = call(ops[i], json.loads(ops[i + 1]))
    print(json.dumps({"op": ops[i], "ok": r.get("ok"), "error": r.get("error"), "result": r.get("result") if ops[i] != "state" else "..."}))
