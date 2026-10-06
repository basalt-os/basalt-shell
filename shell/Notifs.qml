pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Services.Notifications

// The freedesktop notification daemon (org.freedesktop.Notifications)
// lives in the shell, so every app's notifications use the theme. Agents'
// notification.show requests arrive from the daemon and are shown the
// same way.
Singleton {
    id: n
    property var list: []     // newest first: {key, appName, appIcon, summary, body, urgency, time, ref, actions}
    property var popups: []
    property int unread: 0
    property bool dnd: false
    property int _key: 1

    function _push(entry) {
        entry.key = n._key++;
        entry.time = new Date();
        n.list = [entry].concat(n.list).slice(0, 100);
        if (!Ui.drawer) n.unread++;
        if (!n.dnd || entry.urgency === "critical") {
            n.popups = [entry].concat(n.popups).slice(0, 4);
        }
    }
    function dismissPopup(key) { n.popups = n.popups.filter(p => p.key !== key); }
    function remove(key) {
        const e = n.list.find(p => p.key === key);
        if (e && e.ref) { try { e.ref.dismiss(); } catch (err) {} }
        n.list = n.list.filter(p => p.key !== key);
        dismissPopup(key);
    }
    function clear() {
        for (const e of n.list) if (e.ref) { try { e.ref.dismiss(); } catch (err) {} }
        n.list = []; n.popups = []; n.unread = 0;
    }

    NotificationServer {
        id: server
        keepOnReload: true
        bodySupported: true
        bodyMarkupSupported: false
        actionsSupported: true
        imageSupported: true
        persistenceSupported: true
        onNotification: notif => {
            notif.tracked = true;
            const urg = notif.urgency === NotificationUrgency.Critical ? "critical" : (notif.urgency === NotificationUrgency.Low ? "low" : "normal");
            const acts = [];
            for (const a of notif.actions) acts.push({ id: a.identifier, text: a.text, ref: a });
            n._push({ appName: notif.appName || "App", appIcon: notif.appIcon || notif.desktopEntry || "", image: notif.image || "",
                      summary: notif.summary, body: notif.body, urgency: urg, ref: notif, actions: acts, agent: false });
        }
    }

    Connections {
        target: Bus
        function onNotify(d) {
            // A notification of the daemon may name a settings page to open
            // (Additional drivers after a fallback).
            const acts = d.page ? [{ id: "open", text: Tr.t("Open"), ref: { invoke: () => Ui.open("settings", d.page) } }] : [];
            n._push({ appName: d.app || "Basalt assistant", appIcon: "", image: "", summary: d.summary, body: d.body || "",
                      urgency: d.urgency || "normal", ref: null, actions: acts, agent: true });
        }
    }
    Connections {
        target: Ui
        function onDrawerChanged() { if (Ui.drawer) n.unread = 0; }
    }
}
