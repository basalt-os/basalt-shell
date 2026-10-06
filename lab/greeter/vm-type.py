#!/usr/bin/env python3
# vm-type.py [--enter] TEXT | --key KEY...: type into the lab VM through
# the QEMU monitor (sendkey sends scancodes; VNC typing makes QEMU toggle
# Caps Lock to match the case of each letter, which a login test must not
# have). MONITOR: the monitor socket (default vm/monitor.sock).
import os, socket, sys, time

m = {" ": "spc", "-": "minus", "=": "equal", "[": "bracket_left", "]": "bracket_right", ";": "semicolon",
     "'": "apostrophe", "`": "grave_accent", ",": "comma", ".": "dot", "/": "slash", "\n": "ret"}
sh = {"_": "minus", "+": "equal", "{": "bracket_left", "}": "bracket_right", ":": "semicolon", "\"": "apostrophe",
      "~": "grave_accent", "|": "backslash", "<": "comma", ">": "dot", "?": "slash", "!": "1", "@": "2", "#": "3",
      "$": "4", "%": "5", "^": "6", "&": "7", "*": "8", "(": "9", ")": "0"}
s = socket.socket(socket.AF_UNIX)
s.connect(os.environ.get("MONITOR", "vm/monitor.sock"))
time.sleep(0.3)
s.recv(65536)


def send(k):
    s.sendall(("sendkey " + k + "\n").encode())
    time.sleep(0.05)


args = sys.argv[1:]
if args and args[0] == "--key":
    for k in args[1:]:
        send(k)
else:
    enter = bool(args) and args[0] == "--enter"
    text = args[1] if enter else (args[0] if args else "")
    for ch in text + ("\n" if enter else ""):
        if ch.isalpha() and ch.isupper():
            send("shift-" + ch.lower())
        elif ch.isalnum():
            send(ch)
        elif ch in sh:
            send("shift-" + sh[ch])
        else:
            send(m[ch])
time.sleep(0.2)
s.close()
