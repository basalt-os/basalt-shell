# Voice and the read-only skills

Status: spike (2026-10). Push to talk, local speech in both directions,
and three read-only skills (find files, read and summarize e-mail, read
and summarize a web page), reachable by voice and from the command bar. The lab, the tests and the
benchmarks are in `lab/voice/`.

## What the person does

- Hold Super+V (or the microphone button in the panel), speak, release.
  A card under the panel says the microphone is open while the key is
  held, then shows what was heard; the answer appears in the command bar
  and a short version is spoken. On niri (no key-release bindings)
  Super+V toggles; the panel button is hold to talk everywhere.
- The same requests can be typed in the command bar (Super+A): "find the
  PDF the bank sent last month", "what did Ana say in her last email?",
  "summarize news.lab.test", "open result 2".
- The first time a skill needs a folder, a mailbox or a site, the bar
  shows a permission ("Let the assistant read and search the files in
  ~/Documents, ~/Downloads, ~/Desktop for 1 hour"), with Allow and Don't
  allow. Permissions end by themselves; the panel shows the ones in force
  and the bar lists them with End buttons. "Allow access to my Documents
  folder for 10 minutes" and "revoke access" work as requests too.

## Pieces

```
 person: Super+V / panel button                       command bar (typed)
        |                                                    |
 Quickshell UI (basalt_shell_ui_t) --voice.press/release--> basalt-shelld (basalt_shell_t)
                                                     |   |   (ask: rules first, skills, model)
               basalt-voiced (basalt_voice_t) <------+   |
               pw-record, whisper.cpp + Silero VAD,       |  one worker per job, in a session:
               Piper, pw-play; no network                 |  systemd scope + basalt-resolver
                                                          v  (default deny, allowlist = grants)
                         basalt-skill-index (basalt_skill_index_t)   reads granted folders -> index
                         basalt-skill       (basalt_skill_t, agent)  search / IMAP / headless Chromium
```

| Piece | What |
|---|---|
| `basalt-voiced` | the voice service (user unit `basalt-voice.service`, part of the session). Socket `$XDG_RUNTIME_DIR/basalt-voice/voice.sock`, open only to the shell daemon (SELinux context of the peer). `listen` opens a PipeWire capture stream named "Basalt voice"; `stop` closes it, runs whisper.cpp (`whisper-cli`, Silero VAD) on the utterance and returns the text; `speak` synthesizes sentence by sentence with a warm Piper process and plays them; `hush` stops speaking. A hold is cut at 30 s. |
| skills engine (in the daemon) | `internal/skills`: routes a request by fixed rules, plans the search from the request (the model adds synonyms and kinds; the time range comes from fixed rules), checks the grants, runs a worker, summarizes with the model, filters the model's output, composes the answer and the spoken text. |
| `basalt-skill-index` | builds the file index of the granted folders: metadata, text of PDFs (poppler), Office and OpenDocument files, HTML (visible and hidden text), plain text, with the guard's findings per file. No symlinks, no hidden files, never `~/.ssh`, `~/.gnupg`, keyrings, browser profiles. |
| `basalt-skill` | searches the index (BM25 over name, title and text, with kind and time filters), reads a mailbox (read-only IMAP client: `EXAMINE` and `BODY.PEEK` only, the command set is closed), reads a page (headless Chromium over a DevTools pipe, throw-away profile, every request checked). |
| `internal/guard` | content is data: finds instructions addressed to an assistant, hidden text, invisible and look-alike characters, exfiltration links and images, deceptive links; cleans content for the model; filters the model's output. |

## Security model

1. The plan comes only from the person's words. Routing, the search
   terms and the scope are decided from the request before any content
   is read. After content is read the model is only asked to summarize
   (or to pick a number among results); it has no tool to call and its
   answer is a constrained JSON string.
2. No action without the person. The skills are read only. The only
   actions near them, a grant and opening a found file, are typed
   actions (`grant.add`, `file.open`) proposed from the person's request
   and confirmed in the shell; content cannot produce them.
3. Scope by consent. A worker gets only the scope of an active grant:
   the index covers the granted folders, the mail job the granted
   account, the web job the granted host. Grants expire (default 1 hour)
   and the activity log records them.
4. Network by allowlist, in the kernel. Each worker runs in its own
   systemd scope; the daemon registers the scope's slice with
   basalt-resolver before the worker gets its job, with an allowlist made
   of the grant (`news.lab.test:80,443`, `imap.lab.test:143`) and
   loopback off. Every other name is refused by the resolver and every
   other address dropped by nftables, and both are recorded in
   basalt-ledger. The browser refuses the same requests first (and any
   request that is not GET), and reports them. The file jobs get a session
   with an empty allowlist. Without basalt-resolver, network jobs refuse
   to run.
5. Confinement. `basalt_skill_t` is an agent domain (the family's
   `neverallow` rules apply: no credentials, no home, no escalation); it
   may read the index and connect to the web, IMAP and DNS ports.
   `basalt_skill_index_t` may read home content, never key or keyring
   types, and has no network. `basalt_voice_t` may use PipeWire and run
   its speech programs; no network, no home files. Only the shell daemon
   may drive the voice service; only the shell UI may ask the daemon to
   open the microphone (`voice.press` is UI only, like confirming).
6. Plain text everywhere. The shell renders every text as plain text
   (`Txt` sets `Text.PlainText`), so neither content nor a model can make
   the UI load a remote image or a link. The output filter removes links,
   images, markup, code and addresses from the model's text.
7. Warnings. Every finding is shown on the result ("Contains
   instructions addressed to the assistant. Treated as data; nothing in
   it was followed."), and spoken.
8. Content cannot promote itself. A file whose text tries to instruct the
   assistant is listed after clean files, whatever the model picked.

## Model

The skills use the command bar's model settings (the `[translator]`
section of `/etc/basalt/assistant.conf`, the local `basalt-llm` service
through the read helper). A remote endpoint is used only with
`allow_remote`; each answer says which model answered each step and
whether it ran on this machine. The JSON schemas do not constrain string
lengths (llama.cpp's grammar for a bounded repetition made sampling about
ten times slower); the output filter clips instead.

## Configuration

- `/etc/basalt/voice.conf`: speech to text and text to speech programs and
  models, threads, hold limit.
- `~/.config/basalt-shell/skills.conf`: mail accounts (password in a
  private file, mode 0600), named sites, the folders a file grant offers.

## Lab

`lab/voice/`: the VM (`run-voice-vm.sh`, own swtpm), the lab services
(`vm-lab-setup.sh`: lab DNS, web server with request log, dovecot with
the seeded mailbox, the corpus), deploy (`deploy.sh`), the hostile corpus
(`corpus/make-corpus.py`), the tests (`tests/injection-matrix.py`,
`tests/consent-test.py`, `tests/escape-test.sh`), the speech benchmarks
(`speech/`) and the lab microphone (`demo-tools/lab-say`: a PipeWire pipe
source and ydotool holding Super+V).

## Translation (ADR 0014)

Every string a person reads or hears goes through a catalog: `qsTr` in
the QML, `internal/i18n` (`G` for a whole sentence with placeholders, `N`
for plurals) in Go. English is the reference; catalogs are read from
`/usr/share/basalt-shell/locale/<lang>.json` (the spike has no
translations yet; gettext `.po` files and extraction tooling are the
product step). The speech models are English only (Whisper `.en`, the
Piper voices): when the session's language is not English, the first
spoken answer says that answers are spoken in English.

## Push to talk and focus

The spike sends every utterance to the assistant (the command bar). The
recommended routing, not built yet: dictation into a focused text field
through the Wayland input-method protocol, the assistant when no text
field has focus or the utterance is a command; one microphone owner with
a visible state; actions still previewed and confirmed.
