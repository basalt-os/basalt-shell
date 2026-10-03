pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io

// Network state from NetworkManager (nmcli), for wired and wireless
// devices alike. Quickshell's Networking module covers Wi-Fi details.
Singleton {
    id: net
    property string kind: "none"      // ethernet, wifi, none
    property string name: ""          // connection name
    property bool online: kind !== "none"

    Process {
        id: proc
        command: ["nmcli", "-t", "-f", "TYPE,STATE,CONNECTION", "device"]
        stdout: StdioCollector {
            onStreamFinished: {
                let k = "none", n = "";
                for (const line of text.split("\n")) {
                    const f = line.split(":");
                    if (f.length < 3 || f[1] !== "connected") continue;
                    if (f[0] === "wifi" || (f[0] === "ethernet" && k !== "wifi")) { k = f[0]; n = f.slice(2).join(":"); }
                }
                net.kind = k; net.name = n;
            }
        }
    }
    Timer { interval: 8000; running: true; repeat: true; triggeredOnStart: true; onTriggered: proc.running = true }
}
