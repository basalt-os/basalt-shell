#!/usr/bin/env python3
"""Run a program in a network session like the shell's skill runner: a
systemd scope in its own slice, registered with basalt-resolver (default
deny, the given allowlist, loopback off) before the program is let go.

    in-session.py ALLOW[,ALLOW] PROGRAM [ARGS...]
"""
import json, os, secrets, socket, subprocess, sys, time

allow = [a for a in sys.argv[1].split(",") if a]
hexid = secrets.token_hex(5)
sess = "sk-" + hexid
slice_ = "basaltskill-%s.slice" % hexid
# The program waits for a line on stdin before running (sh read), so the
# rules are in place first.
cmd = ["systemd-run", "--user", "--scope", "--quiet", "--collect", "--slice=" + slice_, "--unit=basaltskill-%s-probe" % hexid, "--",
       "/bin/sh", "-c", 'read go; exec "$@"', "sh"] + sys.argv[2:]
p = subprocess.Popen(cmd, stdin=subprocess.PIPE, env=dict(os.environ, GODEBUG="netdns=go"))
cg = ""
for _ in range(50):
    try:
        cg = [l[3:] for l in open("/proc/%d/cgroup" % p.pid).read().splitlines() if l.startswith("0::")][0]
        if slice_ in cg:
            break
    except (OSError, IndexError):
        pass
    time.sleep(0.1)
sl = os.path.dirname(cg)
def resolver(req):
    s = socket.socket(socket.AF_UNIX); s.connect("/run/basalt-resolver/control.sock")
    s.sendall((json.dumps(req) + "\n").encode()); r = json.loads(s.makefile().readline()); s.close(); return r
entries = [e + ":80,443 private" if e.endswith(".test") else e for e in allow]
r = resolver({"op": "register", "session": sess, "profile": "lab-probe", "mode": "skill", "app": "lab probe", "cgroup": sl, "allow": entries, "loopback": False})
print(json.dumps({"attempt": "register session", "allowed": r.get("ok", False), "detail": json.dumps(r)}), flush=True)
p.stdin.write(b"go\n"); p.stdin.close()
p.wait()
resolver({"op": "end", "session": sess})
print(json.dumps({"attempt": "session", "allowed": True, "detail": sess}), flush=True)
