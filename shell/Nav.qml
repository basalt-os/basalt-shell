pragma Singleton

import QtQuick
import Quickshell

// Keyboard navigation shared by every surface (docs/design.md, Keyboard
// and focus). Tab and Shift+Tab move between groups and controls in
// visual order (Qt's focus chain); arrows move inside a group: a row of
// choices, a menu, a list, the Settings sidebar. A group is any item with
// `navRoving: true` and `tabStop: null` (NavRow, NavColumn, NavFlow): its
// controls share one Tab stop (the one focused last, else the selected
// one, else the first), the roving tab stop of the WAI-ARIA patterns.
Singleton {
    id: nav

    // Can this item take the keyboard? Controls built on Pressable, Field
    // and Slider say so with `navigable`; other items with activeFocusOnTab.
    function canFocus(it) {
        // Not the size: a surface that is opening may not be laid out yet.
        if (!it || !it.visible || !it.enabled) return false;
        if (it.navigable !== undefined) return it.navigable === true;
        return it.activeFocusOnTab === true;
    }

    // The focusable controls under root, in tree order (the visual order
    // of Rows, Columns, Flows and Layouts). A control's own children are
    // not searched: a control is one stop. A group nested inside root
    // counts as one stop too: the control holding the focus, else its
    // Tab stop.
    function items(root) {
        const out = [];
        const walk = (it, top) => {
            const kids = it ? it.children : null;
            if (!kids) return;
            if (!top && it.navRoving === true) {
                const inner = [];
                for (let i = 0; i < kids.length; i++)
                    if (kids[i] && kids[i].visible && canFocus(kids[i])) inner.push(kids[i]);
                const pick = inner.find(holdsFocus) || inner.find(x => isStop(it, x)) || inner[0];
                if (pick) out.push(pick);
                return;
            }
            for (let i = 0; i < kids.length; i++) {
                const c = kids[i];
                if (!c || !c.visible) continue;
                if (canFocus(c)) out.push(c);
                else walk(c, false);
            }
        };
        walk(root, true);
        return out;
    }

    // Is it (or something inside it) the focused item?
    function holdsFocus(it) {
        if (!it) return false;
        if (it.activeFocus) return true;
        if (it.input && it.input.activeFocus) return true;
        return false;
    }

    function focus(it, reason) {
        if (!it) return false;
        if (typeof it.focusInput === "function") it.focusInput();
        else it.forceActiveFocus(reason === undefined ? Qt.OtherFocusReason : reason);
        return true;
    }

    // step moves the focus delta controls forward or back inside root;
    // false when there is nothing there (at an edge without wrap), so the
    // key can go on to the enclosing group.
    function step(root, delta, wrap) {
        const list = items(root);
        if (list.length === 0) return false;
        const i = list.findIndex(holdsFocus);
        let j;
        if (i < 0) j = delta > 0 ? 0 : list.length - 1;
        else {
            j = i + delta;
            if (j < 0 || j >= list.length) {
                if (!wrap) return false;
                j = ((j % list.length) + list.length) % list.length;
            }
        }
        if (j === i) return false;
        return focus(list[j], delta > 0 ? Qt.TabFocusReason : Qt.BacktabFocusReason);
    }

    // first / last control inside root.
    function edge(root, last) {
        const list = items(root);
        if (list.length === 0) return false;
        return focus(last ? list[list.length - 1] : list[0], last ? Qt.BacktabFocusReason : Qt.TabFocusReason);
    }

    // The control named key (its navKey, else its e2e name) inside root,
    // or null.
    function find(root, key) {
        if (!key) return null;
        const hit = items(root).find(x => (x.navKey || x.e2e) === key);
        return hit || null;
    }

    // resetStops forgets the last focused control of every group under
    // root: a surface that opens again starts on the selected or first one.
    function resetStops(root) {
        const walk = it => {
            if (!it) return;
            if (it.navRoving === true) it.tabStop = null;
            const kids = it.children;
            if (kids) for (let i = 0; i < kids.length; i++) walk(kids[i]);
        };
        walk(root);
    }

    // initial focuses the control named key, else the selected one of the
    // first group, else the first control; else root itself, so keys
    // (Escape) still reach the surface. Groups start again from their
    // selected (or first) control.
    function initial(root, key, tries) {
        if (!root) return false;
        // A surface that has just been asked to show may not be visible
        // yet (its controls report invisible): try again a moment later.
        if (!root.visible && (tries || 0) < 15) {
            nav.pending = { root: root, key: key, tries: (tries || 0) + 1 };
            retry.restart();
            return false;
        }
        resetStops(root);
        const named = find(root, key);
        if (named) return focus(named, Qt.TabFocusReason);
        const list = items(root);
        if (list.length > 0) {
            const g = list[0].parent;
            const pick = g && g.navRoving === true ? (list.find(x => x.parent === g && isStop(g, x)) || list[0]) : list[0];
            return focus(pick, Qt.TabFocusReason);
        }
        root.forceActiveFocus();
        return false;
    }
    property var pending: null
    Timer {
        id: retry
        interval: 40
        onTriggered: {
            const p = nav.pending;
            nav.pending = null;
            if (p) nav.initial(p.root, p.key, p.tries);
        }
    }

    // Roving tab stop: inside a group only one control is in the Tab chain.
    function isStop(group, it) {
        if (!group || group.navRoving !== true) return true;
        const ts = group.tabStop;
        if (ts && ts !== it && ts.parent === group && ts.visible && ts.navigable === true) return false;
        if (ts === it) return true;
        const kids = group.children;
        let first = null, chosen = null;
        for (let i = 0; i < kids.length; i++) {
            const c = kids[i];
            if (!c || c.navigable !== true || !c.visible) continue;
            if (first === null) first = c;
            if (chosen === null && c.active === true) chosen = c;
        }
        return it === (chosen || first);
    }
    // A control of a group took the focus: it becomes the group's stop.
    function noteFocus(it) {
        const g = it ? it.parent : null;
        if (g && g.navRoving === true) g.tabStop = it;
    }

    // Keys of a group: orientation "horizontal" (Left, Right), "vertical"
    // (Up, Down) or "grid" (all four, columns per row), plus Home and End;
    // with typeAhead, a letter goes to the next control whose label
    // starts with it. Right to left layouts swap Left and Right.
    function groupKey(group, e, orientation, columns, wrap, typeAhead) {
        const rtl = Qt.application.layoutDirection === Qt.RightToLeft;
        const h = orientation === "horizontal" || orientation === "grid";
        const v = orientation === "vertical" || orientation === "grid";
        const rowStep = orientation === "grid" ? Math.max(1, columns || 1) : 1;
        let done = false;
        if (e.modifiers & (Qt.ControlModifier | Qt.AltModifier | Qt.MetaModifier)) { e.accepted = false; return; }
        switch (e.key) {
        case Qt.Key_Left: if (h) done = step(group, rtl ? 1 : -1, wrap); break;
        case Qt.Key_Right: if (h) done = step(group, rtl ? -1 : 1, wrap); break;
        case Qt.Key_Up: if (v) done = step(group, -rowStep, wrap && rowStep === 1); break;
        case Qt.Key_Down: if (v) done = step(group, rowStep, wrap && rowStep === 1); break;
        case Qt.Key_Home: done = edge(group, false); break;
        case Qt.Key_End: done = edge(group, true); break;
        default:
            if (typeAhead && e.text && e.text.length === 1 && /\S/.test(e.text)) done = jumpTo(group, e.text);
        }
        if (done) Ui.focusVisible = true;
        e.accepted = done;
    }

    // Type-ahead: the next control after the focused one whose label
    // (accessibleName, else text) starts with ch.
    function jumpTo(group, ch) {
        const list = items(group);
        if (list.length === 0) return false;
        const c = ch.toLocaleLowerCase();
        const i = list.findIndex(holdsFocus);
        for (let k = 1; k <= list.length; k++) {
            const it = list[(Math.max(i, -1) + k + list.length) % list.length];
            const label = String(it.accessibleName || it.text || "").toLocaleLowerCase();
            if (label.startsWith(c)) return focus(it, Qt.TabFocusReason);
        }
        return false;
    }

    // Keys of a scrolling area that holds no controls (a license, a log, a
    // report): Up, Down, Page Up, Page Down, Home, End scroll it.
    function scrollKey(flick, e) {
        if (!flick) { e.accepted = false; return; }
        const line = Theme.fontSize * 2.4;
        const page = Math.max(line, flick.height - line);
        const max = Math.max(0, flick.contentHeight - flick.height);
        let y = flick.contentY;
        switch (e.key) {
        case Qt.Key_Up: y -= line; break;
        case Qt.Key_Down: y += line; break;
        case Qt.Key_PageUp: y -= page; break;
        case Qt.Key_PageDown: y += page; break;
        case Qt.Key_Home: y = 0; break;
        case Qt.Key_End: y = max; break;
        default: e.accepted = false; return;
        }
        Ui.focusVisible = true;
        flick.contentY = Math.max(0, Math.min(max, y));
        e.accepted = true;
    }

    // reveal scrolls every Flickable around a newly focused control so the
    // control (and its focus ring) is in view.
    function reveal(it) {
        if (!it) return;
        const pad = Theme.focusWidth + Theme.focusOffset + Theme.s2;
        let p = it.parent;
        while (p) {
            if (p.contentY !== undefined && p.contentHeight !== undefined && p.flicking !== undefined && p.contentItem) {
                const r = it.mapToItem(p.contentItem, 0, 0);
                const top = r.y - pad, bottom = r.y + it.height + pad;
                const min = p.originY || 0;
                const max = Math.max(min, min + p.contentHeight - p.height);
                if (top < p.contentY) p.contentY = Math.max(min, top);
                else if (bottom > p.contentY + p.height) p.contentY = Math.min(max, bottom - p.height);
            }
            p = p.parent;
        }
    }
}
