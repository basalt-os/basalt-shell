#!/usr/bin/env python3
"""Score `basalt-shelld translate` output against desktop-requests.tsv.

    basalt-shelld translate < reqs.txt > out.jsonl; score-translator.py desktop-requests.tsv out.jsonl
Prints per-request results and accuracy for the model and for the rules.
"""
import json, re, sys

exp = {}
for line in open(sys.argv[1]):
    if line.startswith("#") or not line.strip():
        continue
    r, e = line.rstrip("\n").split("\t")
    exp[r] = e

def outcome(calls, system, none):
    if system:
        return "system"
    if calls:
        return "+".join(dict.fromkeys(c.split(" ")[0] for c in calls))
    return "none" if none else "unknown"

def ok(e, calls, got):
    base, _, detail = e.partition(":")
    if base in ("system", "none"):
        return got == base
    if set(base.split("+")) != set(got.split("+")):
        return False
    if detail:
        text = " ".join(calls)
        m = re.match(r"([\w.]+)([+-])?$", detail)
        key, sign = m.group(1), m.group(2)
        if key not in text:
            return False
        if sign:
            # crude: the rules print the new value; compare against the base theme
            v = re.search(re.escape(key) + r":(\d+(\.\d+)?)", text)
            if not v:
                return False
            val = float(v.group(1))
            base_vals = {"font.size": 11, "radius.md": 10}
            return val > base_vals[key] if sign == "+" else val < base_vals[key]
    return True

n = mok = rok = fok = hok = 0
ms = []
for line in open(sys.argv[2]):
    o = json.loads(line)
    r = o["request"]
    if r not in exp:
        continue
    n += 1
    e = exp[r]
    mg = outcome(o.get("calls"), o.get("ask_system"), o.get("none")) if "error" not in o else "error"
    rr = o.get("rules", {})
    rg = outcome(rr.get("calls"), rr.get("system"), False)
    m_ok = mg != "error" and ok(e, o.get("calls") or [], mg)
    r_ok = ok(e, rr.get("calls") or [], rg)
    mok += m_ok
    rok += r_ok
    # model first, rules when the model is unsure (none, clarify, error)
    f_ok = m_ok or (mg in ("none", "unknown", "error") and r_ok)
    # rules first when they understand all of it, else the model, else rules
    complete = (rg not in ("unknown",) and not rr.get("unknown")) or rg == "system"
    h_ok = r_ok if complete else (m_ok if mg not in ("none", "unknown", "error") or e == "none" else r_ok)
    fok += f_ok
    hok += h_ok
    if o.get("model"):
        ms.append(o["model"]["elapsed_ms"])
    print(f"{'ok ' if m_ok else 'BAD'} {'ok ' if r_ok else 'BAD'}  {r[:48]:48}  expected {e:28} model {mg:28} rules {rg}")
ms.sort()
p50 = ms[len(ms) // 2] if ms else 0
print(f"\nmodel {mok}/{n} ({100 * mok / n:.0f}%), rules {rok}/{n} ({100 * rok / n:.0f}%), "
      f"model then rules {fok}/{n} ({100 * fok / n:.0f}%), rules then model (shipped) {hok}/{n} ({100 * hok / n:.0f}%), model p50 {p50} ms")
