#!/usr/bin/env python3
"""Drive the read-only skills of a basalt-shell daemon over its socket, as
the shell UI would (lab tests: the test daemon runs with
BASALT_SHELL_UI_CHECK=insecure on its own socket, see test-daemon.sh).

    skillctl.py ask "find the PDF the bank sent last month" [--approve] [--json]
    skillctl.py grants | revoke [ID] | voice
    skillctl.py grant folder|mailbox|site TARGET [DURATION]

--approve confirms a permission the request needs (as the person would on
the sheet) and runs the request again.
"""
import json, os, socket, sys

SOCK = os.environ.get("BASALT_SHELL_SOCKET") or os.path.join(os.environ.get("XDG_RUNTIME_DIR", "/run/user/%d" % os.getuid()), "basalt-shell-test", "shell.sock")

class Client:
    def __init__(self, role="ui"):
        self.s = socket.socket(socket.AF_UNIX)
        self.s.connect(SOCK)
        self.f = self.s.makefile("rw")
        self.n = 0
        self.hello = self.call("hello", {"role": role, "client": "skillctl"})

    def call(self, op, args=None, timeout=600):
        self.n += 1
        self.s.settimeout(timeout)
        self.f.write(json.dumps({"id": self.n, "op": op, "args": args or {}}) + "\n")
        self.f.flush()
        while True:
            line = self.f.readline()
            if not line:
                raise RuntimeError("daemon closed the connection")
            m = json.loads(line)
            if m.get("id") == self.n:
                return m

def ask(c, text, approve=False):
    r = c.call("ask", {"text": text})
    res = r.get("result") or {}
    steps = [res]
    if approve and res.get("kind") == "proposal" and res.get("proposal"):
        p = res["proposal"]
        d = c.call("decide", {"id": p["id"], "approve": True})
        steps.append({"decided": d.get("result", {}).get("status"), "error": d.get("error")})
        if res.get("retry"):
            r = c.call("ask", {"text": res["retry"]})
            res = r.get("result") or {}
            steps.append(res)
    return res, steps

def show(res):
    k = res.get("kind")
    sk = res.get("skill") or {}
    if res.get("proposal"):
        print("PROPOSAL:", "; ".join(res["proposal"].get("steps", [])))
    if sk:
        print("SKILL:", sk.get("skill"), "|", sk.get("text") or sk.get("error"))
        for it in sk.get("items") or []:
            print("  %d. %s | %s" % (it["n"], it["title"], it.get("meta", "")))
            if it.get("summary"):
                print("     ", it["summary"])
            if it.get("warning"):
                print("     WARNING:", it["warning"])
        for w in sk.get("warnings") or []:
            print("  WARNING:", w)
        print("  speech:", sk.get("speech"))
        print("  timing:", sk.get("timing"), "model:", sk.get("model"))
        if sk.get("removed"):
            print("  removed from model output:", sk["removed"])
    elif k in ("error", "unknown"):
        print(k.upper() + ":", res.get("error"))
    elif k != "proposal":
        print(json.dumps(res)[:800])

def main():
    a = sys.argv[1:]
    c = Client()
    if not a:
        print(__doc__); return
    if a[0] == "ask":
        res, steps = ask(c, a[1], "--approve" in a)
        if "--json" in a:
            print(json.dumps(steps, indent=1))
        else:
            for s in steps:
                if "decided" in s:
                    print("DECIDED:", s)
                else:
                    show(s)
    elif a[0] == "grants":
        print(json.dumps(c.call("grants")["result"], indent=1))
    elif a[0] == "revoke":
        print(c.call("grant.revoke", {"id": a[1] if len(a) > 1 else ""}))
    elif a[0] == "grant":
        r = c.call("propose", {"calls": [{"action": "grant.add", "args": {"kind": a[1], "targets": [a[2]], "duration": a[3] if len(a) > 3 else "1h"}}]})
        if not r.get("ok"):
            print(r); return
        p = r["result"]
        print("PROPOSAL:", p["steps"])
        print(c.call("decide", {"id": p["id"], "approve": True}).get("result", {}).get("status"))
    elif a[0] == "voice":
        print(c.call("voice.status"))

if __name__ == "__main__":
    main()
