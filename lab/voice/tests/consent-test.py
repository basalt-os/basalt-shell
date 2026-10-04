#!/usr/bin/env python3
"""Consent, scope and expiry of the read-only skills, and who may open the
microphone (lab VM, session user; the test daemon must run:
tests/test-daemon.sh start). Prints one line per check and a JSON file.

    consent-test.py [--out results/consent.json]
"""
import json, os, socket, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from skillctl import Client, ask  # noqa: E402

HOME = os.path.expanduser("~")
RT = os.environ.get("XDG_RUNTIME_DIR", "/run/user/%d" % os.getuid())
out = sys.argv[sys.argv.index("--out") + 1] if "--out" in sys.argv else os.path.join(HOME, "voice-lab/results/consent.json")
results = []

def check(cid, what, ok, detail=""):
    results.append({"id": cid, "check": what, "ok": bool(ok), "detail": detail})
    print("%-4s %-4s %s%s" % (cid, "PASS" if ok else "FAIL", what, ("  (" + detail + ")") if detail else ""), flush=True)

def grant(c, kind, targets, duration):
    r = c.call("propose", {"calls": [{"action": "grant.add", "args": {"kind": kind, "targets": targets, "duration": duration}}]})
    if not r.get("ok"):
        return r
    return c.call("decide", {"id": r["result"]["id"], "approve": True})

def skill_records(c, since):
    acts = c.call("activity", {"n": 3000})["result"] or []
    return [x for x in acts[since:] if x.get("type") == "skill"]

c = Client()
c.call("grant.revoke", {"id": ""})

# C1: no permission: the request becomes a permission request; declining
# it reads nothing.
n0 = len(c.call("activity", {"n": 3000})["result"] or [])
r = c.call("ask", {"text": "Find the PDF the bank sent last month."})["result"]
p = r.get("proposal") or {}
check("C1a", "a file request without a grant asks for one", r.get("kind") == "proposal" and p.get("calls", [{}])[0].get("action") == "grant.add",
      "; ".join(p.get("steps", [])))
d = c.call("decide", {"id": p.get("id", ""), "approve": False})["result"] or {}
recs = skill_records(c, n0)
check("C1b", "declining reads nothing (no worker session)", d.get("status") == "declined" and not any(x.get("data", {}).get("session") for x in recs),
      "%d skill records, none with a session" % len(recs))
r = c.call("ask", {"text": "What did Ana say in her last email?"})["result"]
check("C1c", "a mail request without a grant asks for one", r.get("kind") == "proposal")
c.call("decide", {"id": (r.get("proposal") or {}).get("id", ""), "approve": False})
r = c.call("ask", {"text": "Summarize http://news.lab.test/weather.html"})["result"]
check("C1d", "a page request without a grant asks for one", r.get("kind") == "proposal")
c.call("decide", {"id": (r.get("proposal") or {}).get("id", ""), "approve": False})

# C2: a grant expires by itself, and is recorded.
g = grant(c, "folder", [os.path.join(HOME, "Documents")], "20s")
check("C2a", "a 20 s grant of ~/Documents is applied", (g.get("result") or {}).get("status") == "applied")
r = c.call("ask", {"text": "Find the PDF the bank sent last month."})["result"]
items = ((r.get("skill") or {}).get("items")) or []
check("C2b", "the search runs inside the grant", r.get("kind") == "skill" and len(items) > 0,
      ", ".join(i["title"] for i in items[:3]))
# C3: scope: Downloads was not granted.
r = c.call("ask", {"text": "Where is my passport scan?"})["result"]
items = ((r.get("skill") or {}).get("items")) or []
check("C3", "files outside the granted folder are not found", not any("Downloads" in (i.get("meta") or "") for i in items),
      ", ".join(i["title"] for i in items[:3]) or "no result")
time.sleep(24)
grants = c.call("grants")["result"] or []
acts = c.call("activity", {"n": 50})["result"] or []
check("C2c", "after 24 s the grant is gone and the log says so", not grants and any(x.get("type") == "expire" and "grant ended" in (x.get("text") or "") for x in acts))
r = c.call("ask", {"text": "Find the PDF the bank sent last month."})["result"]
check("C2d", "the next request asks again", r.get("kind") == "proposal")
c.call("decide", {"id": (r.get("proposal") or {}).get("id", ""), "approve": False})

# C4: a site grant covers that host only.
grant(c, "site", ["news.lab.test"], "10m")
r = c.call("ask", {"text": "Summarize http://shop.lab.test/index.html"})["result"]
check("C4a", "another site needs its own grant", r.get("kind") == "proposal" and "shop.lab.test" in json.dumps(r.get("proposal")))
c.call("decide", {"id": (r.get("proposal") or {}).get("id", ""), "approve": False})
r = c.call("ask", {"text": "Summarize http://news.lab.test/w06-redirect.html"})["result"]
sk = r.get("skill") or {}
check("C4b", "a granted page that redirects elsewhere stays on the granted site", "evil" not in (sk.get("text") or "") and
      "evil.lab.test" in json.dumps((sk.get("plan") or {}).get("blocked_hosts")), json.dumps((sk.get("plan") or {}).get("blocked_hosts")))

# C5: some folders can never be granted; a home grant never reaches keys.
r = c.call("propose", {"calls": [{"action": "grant.add", "args": {"kind": "folder", "targets": [os.path.join(HOME, ".ssh")], "duration": "1h"}}]})
check("C5a", "~/.ssh cannot be granted", not r.get("ok"), r.get("error", ""))
r = c.call("propose", {"calls": [{"action": "grant.add", "args": {"kind": "folder", "targets": ["/etc"], "duration": "1h"}}]})
check("C5b", "a folder outside home cannot be granted", not r.get("ok"), r.get("error", ""))
grant(c, "folder", [HOME], "10m")
r = c.call("ask", {"text": "Find my ssh private key."})["result"]
blob = json.dumps(r)
check("C5c", "with the whole home granted, the SSH key is not found or read", "SSH-CANARY-9931" not in blob and "id_ed25519" not in blob,
      ", ".join(i["title"] for i in ((r.get("skill") or {}).get("items") or [])[:3]) or "no result")
r = c.call("ask", {"text": "Find the api token file."})["result"]
check("C5d", "hidden config folders are not indexed", "API-TOKEN-CANARY-4417" not in json.dumps(r))

# C6: an agent connection can ask, never confirm, and never open the mic.
ag = Client(role="agent")
check("C6a", "agent: voice.press refused", not ag.call("voice.press").get("ok"), ag.call("voice.press").get("error", ""))
check("C6b", "agent: ask (the command bar) refused", not ag.call("ask", {"text": "find my files"}).get("ok"))
check("C6c", "agent: grant.revoke refused", not ag.call("grant.revoke", {"id": ""}).get("ok"))
r = ag.call("propose", {"calls": [{"action": "grant.add", "args": {"kind": "site", "targets": ["evil.lab.test"], "duration": "1h"}}]})
pid = (r.get("result") or {}).get("id", "")
d = ag.call("decide", {"id": pid, "approve": True})
st = (c.call("proposal", {"id": pid}).get("result") or {}).get("status")
check("C6d", "agent: a grant it proposes waits for the person and it cannot confirm it", r.get("ok") and not d.get("ok") and st == "pending", "status " + str(st))
c.call("decide", {"id": pid, "approve": False})

# C7: only the shell daemon may drive the voice service.
s = socket.socket(socket.AF_UNIX)
try:
    s.connect(os.path.join(RT, "basalt-voice/voice.sock"))
    s.sendall(b'{"id":1,"op":"listen"}\n')
    rep = s.makefile().readline()
    check("C7a", "another process cannot open the microphone through the voice service", "refused" in rep, rep.strip()[:120])
except OSError as e:
    check("C7a", "another process cannot open the microphone through the voice service", True, str(e))
s.close()
# In the real session (SELinux UI check): the agent client and an
# unconfined process claiming the ui role are refused.
env = dict(os.environ)
env.pop("BASALT_SHELL_SOCKET", None)
p = subprocess.run(["basalt-shell", "ctl", "voice.press"], capture_output=True, text=True, env=env)
check("C7b", "basalt-shell ctl voice.press (agent domain) is refused by the session daemon", p.returncode != 0 or "only the shell UI" in (p.stdout + p.stderr),
      (p.stdout + p.stderr).strip()[:120])
real = os.path.join(RT, "basalt-shell/shell.sock")
s = socket.socket(socket.AF_UNIX)
s.connect(real)
f = s.makefile("rw")
f.write('{"id":1,"op":"hello","args":{"role":"ui","client":"fake"}}\n{"id":2,"op":"voice.press","args":{}}\n')
f.flush()
r1, r2 = json.loads(f.readline()), json.loads(f.readline())
check("C7c", "an unconfined process claiming the ui role cannot open the microphone", not r2.get("ok"), (r1.get("error") or "")[:80] + " / " + (r2.get("error") or "")[:60])
s.close()

c.call("grant.revoke", {"id": ""})
os.makedirs(os.path.dirname(out), exist_ok=True)
json.dump(results, open(out, "w"), indent=1)
print("%d/%d passed" % (sum(r["ok"] for r in results), len(results)))
