#!/usr/bin/env python3
"""Minimal MCP client for demos and tests: starts `basalt-shell mcp`,
initializes, optionally lists tools, calls one tool and prints the result.

  mcp-call.py TOOL '{"arg": "value"}'
  mcp-call.py --list
"""
import json
import subprocess
import sys


def main():
    proc = subprocess.Popen(["basalt-shell", "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
    n = 0

    def rpc(method, params=None, notify=False):
        nonlocal n
        msg = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            msg["params"] = params
        if not notify:
            n += 1
            msg["id"] = n
        proc.stdin.write(json.dumps(msg) + "\n")
        proc.stdin.flush()
        if notify:
            return None
        return json.loads(proc.stdout.readline())

    init = rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                              "clientInfo": {"name": "demo-agent", "version": "1"}})
    print("server:", init["result"]["serverInfo"], file=sys.stderr)
    rpc("notifications/initialized", notify=True)
    if sys.argv[1:] == ["--list"]:
        for t in rpc("tools/list")["result"]["tools"]:
            print(f'{t["name"]:22} {"read" if t["annotations"]["readOnlyHint"] else "write"}  {t["description"][:90]}')
        return
    tool, args = sys.argv[1], json.loads(sys.argv[2] if len(sys.argv) > 2 else "{}")
    res = rpc("tools/call", {"name": tool, "arguments": args})["result"]
    print(("ERROR " if res.get("isError") else "") + res["content"][0]["text"])
    proc.stdin.close()
    proc.wait()


if __name__ == "__main__":
    main()
