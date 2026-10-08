// Pure functions of Settings, Updates and channels (UpdatesSettings.qml):
// whether a channel can be turned on here, and the one line a person
// reads when a change did not work. No QML or Quickshell types here, so
// the same file runs in the shell (imported as a JavaScript resource) and
// in the unit tests (shell/tests, Node).

// The channels the basalt-nonfree-release package defines.
const nonfreeChannels = ["basalt-nonfree", "basalt-nonfree-testing"];

// channelAvailable: can this channel be turned on here? Channels defined
// by basalt-release always can. A channel defined by basalt-nonfree-release
// needs two things:
// - its own repository published: the daemon fetched its repomd.xml
//   (c.published, from channels.state). basalt-nonfree-release can be
//   installed long before a driver build is published, and a channel
//   turned on before that leaves dnf failing on a 404;
// - the definition: defined here already, or basalt-nonfree-release
//   installed or offered by the enabled repositories according to dnf's
//   cached metadata (the assistant's drivers report,
//   state.nonfree_available and state.release_package).
// Until then the page shows it as not available yet. Anything unknown (a
// report not read yet, an assistant or daemon without the field) counts as
// not available. A channel that is on can always be turned off.
function channelAvailable(c, chan, drv) {
    if (!c) return false;
    if (nonfreeChannels.indexOf(c.id) < 0) return true;
    if (c.enabled) return true;
    if (c.published !== true) return false;
    if (c.defined) return true;
    const st = drv && drv.state ? drv.state : null;
    if (!st) return false;
    return st.release_package === true || st.nonfree_available === true;
}

// failureKind reads the assistant's report of a change that did not work
// (or an error from storing the proposal) and names what went wrong, for
// a one line message; the report itself stays under Details.
//   unpublished: a package the change needs is not in any repository
//   network:     the repositories could not be reached
//   signature:   a package or the repository metadata failed its signature check
//   space:       the disk is full
//   refused:     the approval (password) was not given
//   "":          anything else
function failureKind(text) {
    const t = String(text || "");
    if (/No match for argument|Unable to find a match|No package [^\n]* available|is not published yet/i.test(t)) return "unpublished";
    if (/Curl error|Cannot download|Failed to download metadata|Could not resolve host|Cannot prepare internal mirrorlist|Network is unreachable|Connection timed out/i.test(t)) return "network";
    if (/GPG check FAILED|signature[^\n]*(not ok|invalid|bad)|not signed|Public key for [^\n]* is not installed|repomd\.xml[^\n]*signature/i.test(t)) return "signature";
    if (/No space left on device|Not enough free space|need[^\n]* more space/i.test(t)) return "space";
    if (/Request dismissed|Not authorized|Authentication failed|Error executing command as another user/i.test(t)) return "refused";
    return "";
}

// firstLine: the first non-empty line of an error, cut at a word to at
// most max characters, for a status line (the rest goes under Details).
function firstLine(text, max) {
    const lines = String(text || "").split("\n").map(l => l.trim()).filter(l => l !== "");
    let s = lines.length > 0 ? lines[0] : "";
    const n = max || 160;
    if (s.length > n) {
        s = s.slice(0, n);
        const sp = s.lastIndexOf(" ");
        if (sp > n / 2) s = s.slice(0, sp);
    }
    return s;
}
