#!/usr/bin/env python3
"""Scripted agent driving the desktop through the shell's MCP server.

Stands in for a model: it starts `basalt-shell mcp` (confined in
basalt_agent_mcp_t when the policy is installed) and uses the typed tools
first, then the last-resort tools (screenshot, control session, input).
A "person" thread confirms the sheets with a kernel-level virtual keyboard
(ydotool, lab only) and takes screenshots of what the person sees (grim,
outside the agent's domain).

    agent-demo.py OUTDIR [--no-person] [--wtype]

--wtype: the person presses keys with wtype on WAYLAND_DISPLAY (a headless
session has no input devices; there the person would use the VNC view).
"""
import base64, json, os, subprocess, sys, threading, time

out = sys.argv[1]
person_on = "--no-person" not in sys.argv
use_wtype = "--wtype" in sys.argv
os.makedirs(out, exist_ok=True)
env = dict(os.environ)
mcp = subprocess.Popen(["basalt-shell", "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, env=env)
log = open(os.path.join(out, "agent-demo.log"), "w")
n = 0


def say(*a):
    msg = " ".join(str(x) for x in a)
    print(msg, flush=True)
    log.write(msg + "\n")
    log.flush()


def rpc(method, params=None):
    global n
    n += 1
    mcp.stdin.write(json.dumps({"jsonrpc": "2.0", "id": n, "method": method, "params": params or {}}) + "\n")
    mcp.stdin.flush()
    while True:
        m = json.loads(mcp.stdout.readline())
        if m.get("id") == n:
            return m


def tool(name, args=None):
    t0 = time.time()
    r = rpc("tools/call", {"name": name, "arguments": args or {}})["result"]
    texts, images = [], []
    for c in r["content"]:
        if c["type"] == "text":
            texts.append(c["text"])
        elif c["type"] == "image":
            images.append(base64.b64decode(c["data"]))
    say(f"-> {name} {json.dumps(args or {})} [{time.time() - t0:.1f}s] error={r.get('isError', False)}")
    for t in texts:
        say("   " + t.replace("\n", "\n   ")[:700])
    return r, texts, images


def press(key):
    """Enter or Escape, as the person."""
    if use_wtype:
        subprocess.run(["wtype", "-k", {"enter": "Return", "esc": "Escape"}[key]], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    else:
        code = {"enter": "28", "esc": "1"}[key]
        subprocess.run(["ydotool", "key", code + ":1", code + ":0"], env=dict(env, YDOTOOL_SOCKET="/run/ydotoold.socket"),
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def grim(name):
    subprocess.run(["grim", os.path.join(out, name + ".png")], check=False)


def person(name, delay=2.0, key=True):
    """The person looks at the sheet (screenshot) and presses Enter."""
    def run():
        time.sleep(delay)
        grim(name)
        if key:
            press("enter")
    t = threading.Thread(target=run)
    t.start()
    return t


init = rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "agent-demo"}})
say("server:", init["result"]["serverInfo"])
tools = rpc("tools/list")["result"]["tools"]
say("tools:", len(tools), " ".join(t["name"] for t in tools))

_, t, _ = tool("toplevels_list")
wins = json.loads(t[0]) or []
term = next((w for w in wins if w["app_id"] == "foot"), None)
if term is None:
    say("no foot window: open one first")
    sys.exit(1)

# 1. A screenshot of one window: the person confirms it on a sheet.
p = person("01-sheet-screenshot") if person_on else None
r, t, imgs = tool("screen_capture", {"target": "window", "window": term["id"], "max_width": 1200})
if p: p.join()
if imgs:
    open(os.path.join(out, "02-agent-received-window.png"), "wb").write(imgs[0])

# 2. Input without a control session is refused.
tool("input_type_text", {"text": "should not be typed"})

# 3. A control session: the person allows it on the sheet.
p = person("03-sheet-control", delay=2.5) if person_on else None
tool("agent_control_request", {"reason": "type a command into the terminal (demo of the last-resort tools)", "minutes": 3})
if p: p.join()
time.sleep(1)
grim("04-frame-and-banner")

# 4. Act: click into the terminal, type, press Enter.
cx, cy = term["rect"]["x"] + term["rect"]["width"] // 2, term["rect"]["y"] + term["rect"]["height"] // 2
tool("input_pointer_click", {"x": cx, "y": cy})
tool("input_type_text", {"text": "echo typed by an agent inside a control session"})
tool("input_key", {"keys": "Return"})
time.sleep(0.8)
# 5. Inside the session a screenshot needs no new confirmation.
r, t, imgs = tool("screen_capture", {"target": "output", "max_width": 1280})
if imgs:
    open(os.path.join(out, "05-agent-received-screen.png"), "wb").write(imgs[0])
grim("05-person-view-during-control")

# 6. While the person has a request to answer, input is refused.
subprocess.run(["basalt-shell", "propose", "notification.show", '{"summary": "another request"}', "--wait", "0"], stdout=subprocess.DEVNULL)
tool("input_key", {"keys": "Return"})
_, t, _ = tool("agent_control_status")
p = person("06-sheet-other-request", delay=1.0, key=False) if person_on else None
if p: p.join()
# The person declines it with Escape.
press("esc")
time.sleep(2)

# 7. Done: the agent ends its session; input is refused again.
tool("agent_control_stop")
tool("input_key", {"keys": "Return"})
_, t, _ = tool("activity_recent", {"n": 14})
mcp.stdin.close()
mcp.wait()
