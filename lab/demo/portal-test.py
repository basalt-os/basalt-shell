#!/usr/bin/env python3
"""Exercise xdg-desktop-portal interfaces from the session (lab tests).

  portal-test.py settings      read org.freedesktop.appearance (color-scheme, accent-color)
  portal-test.py screenshot    Screenshot portal, non-interactive: prints the file URI
  portal-test.py screencast    ScreenCast portal: session, source selection (the
                               backend's chooser appears), start; prints the PipeWire node
"""
import sys

from gi.repository import Gio, GLib

bus = Gio.bus_get_sync(Gio.BusType.SESSION, None)
NAME, PATH = "org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop"
loop = GLib.MainLoop()
token_n = 0


def token():
    global token_n
    token_n += 1
    return f"basalt{token_n}"


def request(iface, method, args, timeout=60):
    """Call a portal method that answers through a Request object."""
    tok = token()
    sender = bus.get_unique_name()[1:].replace(".", "_")
    handle = f"/org/freedesktop/portal/desktop/request/{sender}/{tok}"
    result = {}

    def on_response(_c, _s, _p, _i, _sig, params):
        code, res = params.unpack()
        result["code"], result["res"] = code, res
        loop.quit()

    sub = bus.signal_subscribe(NAME, "org.freedesktop.portal.Request", "Response", handle, None,
                               Gio.DBusSignalFlags.NO_MATCH_RULE, on_response)
    opts = args[-1]
    opts["handle_token"] = GLib.Variant("s", tok)
    bus.call_sync(NAME, PATH, iface, method, GLib.Variant(*build(method, args)), None, 0, GLib.MAXINT32, None)
    GLib.timeout_add_seconds(timeout, loop.quit)
    loop.run()
    bus.signal_unsubscribe(sub)
    return result.get("code", -1), result.get("res", {})


def build(method, args):
    sigs = {"Screenshot": "(sa{sv})", "CreateSession": "(a{sv})", "SelectSources": "(oa{sv})", "Start": "(osa{sv})"}
    return sigs[method], tuple(args)


def main():
    what = sys.argv[1] if len(sys.argv) > 1 else "settings"
    if what == "settings":
        for key in ("color-scheme", "accent-color"):
            try:
                v = bus.call_sync(NAME, PATH, "org.freedesktop.portal.Settings", "ReadOne",
                                  GLib.Variant("(ss)", ("org.freedesktop.appearance", key)), None, 0, -1, None)
                print(f"org.freedesktop.appearance {key} = {v.unpack()[0]}")
            except GLib.Error as e:
                print(f"org.freedesktop.appearance {key}: {e.message}")
    elif what in ("screenshot", "screenshot-interactive"):
        code, res = request("org.freedesktop.portal.Screenshot", "Screenshot",
                            ["", {"interactive": GLib.Variant("b", what == "screenshot-interactive")}])
        print("screenshot response", code, res.get("uri", ""), flush=True)
    elif what == "screencast":
        iface = "org.freedesktop.portal.ScreenCast"
        code, res = request(iface, "CreateSession", [{"session_handle_token": GLib.Variant("s", token())}])
        print("CreateSession", code)
        session = res.get("session_handle", "")
        code, _ = request(iface, "SelectSources", [session, {"types": GLib.Variant("u", 1), "multiple": GLib.Variant("b", False)}])
        print("SelectSources", code)
        code, res = request(iface, "Start", [session, "", {}], timeout=90)
        print("Start", code, "streams:", res.get("streams", []), flush=True)
        streams = res.get("streams", [])
        if code == 0 and streams:
            # Pull one frame from the PipeWire stream the portal opened.
            import subprocess
            node = streams[0][0]
            out = sys.argv[2] if len(sys.argv) > 2 else "/tmp/screencast-frame.png"
            # The stream is only visible through the PipeWire connection the
            # portal hands out (OpenPipeWireRemote returns its fd).
            ret, fds = bus.call_with_unix_fd_list_sync(NAME, PATH, iface, "OpenPipeWireRemote",
                                                       GLib.Variant("(oa{sv})", (session, {})), GLib.VariantType("(h)"),
                                                       0, -1, None, None)
            fd = fds.get(ret.unpack()[0])
            # niri offers DMA-BUF only: go through GL to read the frame back;
            # sway (wlr) also offers shared memory.
            gl = ["!", "glupload", "!", "glcolorconvert", "!", "gldownload"]
            r = subprocess.run(["gst-launch-1.0", "-q", "pipewiresrc", f"fd={fd}", f"path={node}", "num-buffers=3"] + gl +
                               ["!", "videoconvert", "!", "pngenc", "snapshot=true", "!", "filesink", f"location={out}"],
                               timeout=30, capture_output=True, text=True, pass_fds=(fd,))
            print("frame", r.returncode, out, r.stderr.strip()[-200:], flush=True)


main()
