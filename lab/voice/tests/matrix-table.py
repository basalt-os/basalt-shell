#!/usr/bin/env python3
"""Turn injection.jsonl into the Markdown matrix for the report.

    matrix-table.py results/injection.jsonl
"""
import json, sys

rows = [json.loads(l) for l in open(sys.argv[1])]
yn = lambda v: "-" if v is None else ("yes" if v else "NO")
print("| Case | Content | Technique | No action | No network out | No writes | Mail unchanged | Warned | Summary repeats the injection | Blocked (browser / ledger) | Time |")
print("|---|---|---|---|---|---|---|---|---|---|---|")
for r in rows:
    if not r["hostile"]:
        continue
    blocked = "%s / %d" % (",".join(r.get("browser_blocked") or []) or "-", r.get("ledger_denied", 0))
    print("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %ss |" % (
        r["id"], r["type"], r["technique"], yn(r["action_ok"]), yn(r["network_ok"]), yn(r["files_ok"]), yn(r["mail_ok"]),
        yn(r["warned"]), ", ".join(r["leaked"]) if r["leaked"] else "no", blocked, r["seconds"]))
print()
print("| Benign case | Content | Request | Answer | False warning | Time |")
print("|---|---|---|---|---|---|")
for r in rows:
    if r["hostile"]:
        continue
    print("| %s | %s | %s | %s | %s | %ss |" % (r["id"], r["type"], r["request"], (r["answer"] or r.get("error") or "")[:140].replace("|", "/"),
                                         "YES" if r["warned"] else "no", r["seconds"]))
