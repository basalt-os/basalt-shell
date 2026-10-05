#!/usr/bin/env python3
"""Check the escape probe's results (escape-test.sh) against what each
domain may do. Every attempt not listed as allowed for a domain must be
denied. Prints a table and exits 1 on any difference.

    escape-expect.py RESULTS_DIR
"""
import json, os, sys

out = sys.argv[1]
# What each domain is allowed to do; everything else must fail.
ALLOWED = {
    # The voice service's own socket is its own runtime directory.
    "basalt_voice_exec_t": {"tool: pw-record (voice)", "tool: whisper-cli (voice)", "connect PipeWire (microphone)", "connect the voice service"},
    "basalt_skill_index_exec_t": {"tool: pdftotext (indexer)", "read a document in ~/Documents", "read the file index"},
    "basalt_skill_exec_t": {"tool: chromium (worker)", "read the file index", "write a program in /tmp", "create a memory file",
                            "register session", "session"},
    # By address too, once the name was resolved in the session (the
    # session's kernel set holds the allowed name's addresses).
    "basalt_skill_send_exec_t": {"connect SMTP by name (mail.example.com:587)", "connect SMTP by address (10.77.0.21:587)",
                                 "write a program in /tmp", "create a memory file",
                                 "register session", "session"},
    "basalt_skill_files_exec_t": {"rename a file in ~/Documents"},
}
# Results that are informational, not attempts.
SKIP = {"domain"}
# Attempts that must be present for each domain (the probe really ran them).
MUST = ["run /bin/sh", "run python3", "pkexec", "sudo", "newgrp", "the dynamic loader running bash",
        "read ~/.ssh/id_ed25519", "connect the Wayland socket"]

bad = 0
rows = []
for dom in ALLOWED:
    path = os.path.join(out, "escape-%s.jsonl" % dom)
    try:
        lines = [json.loads(l) for l in open(path) if l.strip().startswith("{")]
    except OSError:
        print("missing", path)
        bad += 1
        continue
    seen = {r["attempt"] for r in lines}
    for m in MUST:
        if m not in seen:
            print("%s: attempt %r missing (the probe did not run?)" % (dom, m))
            bad += 1
    domain = next((r["detail"] for r in lines if r["attempt"] == "domain"), "?")
    for r in lines:
        a = r["attempt"]
        if a in SKIP:
            continue
        want = a in ALLOWED[dom]
        ok = r["allowed"] == want
        if not ok:
            bad += 1
        rows.append((dom, a, "allowed" if r["allowed"] else "denied", "ok" if ok else "UNEXPECTED", r.get("detail", "")[:70]))
    print("== %s runs as %s" % (dom, domain))
for dom, a, got, verdict, detail in rows:
    print("  %-26s %-50s %-8s %-10s %s" % (dom.replace("_exec_t", ""), a, got, verdict, detail))
print("\n%d unexpected results" % bad)
sys.exit(1 if bad else 0)
