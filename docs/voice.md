# Voice and the skills

Status: 0.4 (2026-10), pre-release. Push to talk, local speech in both
directions, three read-only skills (find files, read and summarize
e-mail, read and summarize a web page) and three acting skills that are
always previewed and confirmed (dictation into the focused text field,
reply to an e-mail, move and rename files), reachable by voice and from
the command bar. Each person chooses their own speech language, speech
model, answer language, voice and language model (Languages below),
independently of the desktop's language. The lab, the tests and the
benchmarks are in `lab/voice/`.

## What the person does

- Hold Super+V (or the microphone button in the panel), speak, release.
  A card under the panel says the microphone is open while the key is
  held, then shows what was heard; the answer appears in the command bar
  and a short version is spoken. On niri (no key-release bindings)
  Super+V toggles; the panel button is hold to talk everywhere.
- The same requests can be typed in the command bar (Super+A): "find the
  PDF the bank sent last month", "what did Ana say in her last email?",
  "summarize news.example.org", "open result 2".
- With a text field focused (any app that supports text input, through
  the Wayland input method), holding Super+V dictates into it: the card
  says "Dictating into Mousepad", the words appear underlined in the
  field, and they are typed only after Insert (or Super+Shift+Return);
  Discard (Super+Shift+BackSpace) drops them. Starting with "assistant"
  sends the words to the assistant instead. Password and PIN fields get
  no dictation.
- "Reply to Priya: the slides will be ready on Friday" shows the reply:
  from and to (the sender of Priya's message, not editable), the subject
  and the text, both editable. Nothing is sent until Send.
- "Move result 2 to Archive", "move my cleanup notes to Archive",
  "rename result 1 to statement-september": the list of moves, confirmed
  with Confirm; "undo" puts the files back. Nothing is deleted or
  replaced.
- The first time a skill needs a folder, a mailbox or a site, the bar
  shows a permission ("Let the assistant read and search the files in
  ~/Documents, ~/Downloads, ~/Desktop for 1 hour"), with Allow and Don't
  allow. Permissions end by themselves; the panel shows the ones in force
  and the bar lists them with End buttons. "Allow access to my Documents
  folder for 10 minutes" and "revoke access" work as requests too.

## Languages

Speech and answers have their own language, set per person in Settings,
Voice and assistant (Super+Comma), independent of the desktop's language:
an English desktop can be spoken to, and answer, in Brazilian Portuguese.

- Speech language: Automatic (a multilingual model detects it; slower,
  and less reliable on short requests) or a language. Whisper's `.en`
  models understand English only: choosing another language with one of
  them is refused with a message, and the Settings page switches to an
  installed multilingual model the administrator allows in the same
  step. Push to talk refuses to open the microphone when the settings
  were edited by hand into that combination, and the card says why.
- The voice card shows the speech language in force while the key is
  held and when speech to text fails.
- Answer language: the person's answer language, else their speech
  language (when it is not Automatic), else the session's language
  (`LANG` of the person's session), else English. The system-wide default
  locale is never forced on anyone. What the assistant composes (results,
  permissions, confirmations, warnings) comes from the catalog of that
  language; what the model writes (summaries, reply drafts) is asked for
  in it (Model below).
- Spoken answers: on or off, and a voice per language. An answer is read
  only by a voice of its own language; without one it is shown and not
  spoken, and the card says so once per language and session. The Piper
  voices allowed today are English only (the basalt-os documentation,
  `docs/voice.md`, says why), so Portuguese answers are shown, not spoken.
- Language model: the local model (the command bar's model settings), or
  a remote model only when the administrator offers one and allows
  remote models, and the person turns on "Allow a remote model".
- Requests: the fixed phrases of the command bar and the skills'
  routing understand English and Brazilian Portuguese ("deixe mais
  escuro", "use o tema lichen", "organize as janelas", "encontre o PDF que
  o banco mandou no mês passado", "o que a Ana disse no último e-mail?",
  "abra o resultado 2", "permita o acesso à minha pasta Documentos por uma
  hora"). "Assistente" works like "assistant" to talk to the assistant
  from a text field. Portuguese words about documents and mail get their
  English equivalents in a search (`banco` also searches `bank`), since
  many file names are English.
- The speech recognizer is told the words of the requests in the speech
  language and the names of the installed themes and of the person's
  contacts.

The person's settings file, written by the Settings page and readable by
hand:

```ini
# ~/.config/basalt/voice-and-assistant.conf
[speech]
language = pt-BR          # auto, or a language tag; empty: the system's default
model = ggml-small-q5_1   # an installed speech model; empty: the system's default

[answers]
language =                # empty: the speech language, then the session's language
spoken = yes

[voices]
en = en_US-ljspeech-medium

[model]
choice = local            # local, or a remote model the administrator lists
allow_remote = no         # the person's own opt-in
#api_key_file = ~/.config/basalt/remote.key   # mode 0600, only for a remote model
```

The administrator's policy stays in `/etc` and is enforced on every
request; a person's choice cannot widen it:

| File | Keys | Enforced by |
|---|---|---|
| `/etc/basalt/voice.conf` | `BASALT_VOICE_LANGUAGE` (default speech language, `en`), `BASALT_VOICE_ALLOWED_MODELS` (speech models and voices a person may choose; empty: every installed one), `BASALT_VOICE_MAX_MODEL_MB` (largest speech model a person may choose), `BASALT_VOICE_MODEL_DIRS` | basalt-voiced, on every utterance and every spoken answer |
| `/etc/basalt/desktop-models.conf` | `[policy] allow_remote`, `[remote NAME]` sections with `label`, `endpoint` (https only) and `model` | the shell daemon, when the settings are saved and on every request |

A value outside the policy is refused when the person saves it, with the
reason; a hand-edited value outside it is not used (the defaults are) and
the Settings page lists the reason. The daemon picks up the person's file
at the next request (it checks the file's modification time): no restart.
A change of `/etc/basalt/voice.conf` needs `systemctl --user restart
basalt-voice` in the person's session.

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
| `basalt-skill-send` | sends one confirmed e-mail: a send-only SMTP client with a closed command set (no VRFY, EXPN, second recipient), TLS required unless the server has a reserved lab name. |
| `basalt-skill-files` | renames and moves files inside a granted folder after confirmation: `openat2` beneath the folder with no symbolic links, `renameat2` with no replace, never a read or a delete. |
| `internal/wlime` | the shell daemon as the session's input method (`zwp_input_method_v2`): knows when a text field has focus and of which kind, shows pre-edit text, commits confirmed text. |
| `internal/ledger` | sends the acting records (with their exact previews), grants, refusals and skill sessions to basalt-ledger. |

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
   of the grant (`news.example.org:80,443`, `imap.example.org:143`) and
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
9. Acting is a typed action with an exact preview, confirmed only in the
   shell UI. `text.insert`, `mail.send` and `files.move` are planned only
   from the person's own words (the command bar or push to talk); an
   agent connection cannot propose them. The confirmation shows exactly
   what will be done (the dictated text; the recipient, subject and
   text; every move), the activity log and basalt-ledger keep that
   preview, and what runs is what was shown (an edit of an e-mail's text
   on the confirmation plans the action again).
10. Replies: the recipient is the sender of the message the person named,
    never an address from the content (a Reply-To that points elsewhere
    is shown as a warning and ignored). The text is the person's words;
    the model only rewrites an instruction about the recipient ("tell
    her..."), and its draft is replaced by the person's words when it has
    links or addresses, copies the message, repeats what the message
    dictates, or adds numbers the person did not say. Plain text, no
    attachment, one recipient.
11. Files: only the files the person named (result numbers or a search
    of their words), only inside one granted folder, never hidden, never
    replacing a file, never deleting; each batch can be undone.
12. Own programs only. The voice and skill domains may run their own
    tools and nothing else: no shell, no interpreter, no setuid helper
    (sudo, su, pkexec, newgrp), no program they wrote, not through the
    dynamic loader (`neverallow` rules, and `no_new_privs`).
13. Push to talk is off while the screen is locked: the compositor does
    not run the shell's key bindings during a session lock, and the
    daemon refuses to open the microphone while a screen locker runs.
14. The person's settings stay theirs. `~/.config/basalt` has its own
    type, `basalt_user_conf_t` (set when the daemon creates the
    directory). The daemon is the only program that reads it; the voice
    service and the skill workers get the values with each request and
    still have no access to home files. `neverallow` rules keep every
    agent domain and the voice and skill domains from writing it. Only
    the shell UI may save new settings (`voice.settings.set`, like
    confirming), each change is checked against the policy and written
    to the activity log.

## Model

The skills use the person's model: the command bar's model settings (the
`[translator]` section of `/etc/basalt/assistant.conf`, the local
`basalt-llm` service through the read helper), or a remote model the
administrator offers in `/etc/basalt/desktop-models.conf` when both the
administrator and the person allow it. Each answer says which model
answered each step and whether it ran on this machine. The JSON schemas
do not constrain string lengths (llama.cpp's grammar for a bounded
repetition made sampling about ten times slower); the output filter clips
instead.

When the answer language is not English, every system prompt that leads
to text the person reads (the summaries, and the command bar's
translator) gets one more line: answer the person in that language, and
keep everything machine-facing exactly as specified, in English: JSON
keys, intent names, enum values, identifiers, theme and setting names,
file names and paths, addresses, quoted names. The prompts and schemas
themselves stay English and the shell checks the structured fields
against the same English identifiers in every language (a translated
identifier, such as a theme called "líquen", is not one, and gets "please
say which one"). The planning steps (search words) get no such line;
they ask for the request's words in its own language and in English. The
output filter keeps text in any script whole (accents, cedillas,
decomposed characters) and still removes links, addresses, markup and
commands.

## Configuration

- `/etc/basalt/voice.conf`: speech to text and text to speech programs and
  models, threads, hold limit, and the policy for the person's choices
  (Languages above).
- `/etc/basalt/desktop-models.conf`: the language models a person may
  choose (Languages above).
- `~/.config/basalt/voice-and-assistant.conf`: the person's own speech
  and answer languages, speech model, voices and model choice.
- `~/.config/basalt-shell/skills.conf`: mail accounts (password in a
  private file, mode 0600; `address`, `name`, `smtp_host`, `smtp_port`,
  `smtp_tls` for replies), named sites, the folders a file grant offers,
  `[contacts] names = ...` (names the speech recognition should spell
  right; the senders of a granted mailbox are added while the grant
  lasts).

## Lab

`lab/voice/`: the VM (`run-voice-vm.sh`, own swtpm), the lab services
(`vm-lab-setup.sh`: lab DNS, web server with request log, dovecot with
the seeded mailbox, the corpus), deploy (`deploy.sh`), the hostile corpus
(`corpus/make-corpus.py`), the tests (`tests/injection-matrix.py`,
`tests/consent-test.py`, `tests/acting-matrix.py`, `tests/escape-test.sh`
with `tests/escape-expect.py`), the SMTP sink and demo names
(`vm-lab-acting.sh`, `demo/demo-home.sh`), the speech benchmarks
(`speech/`) and the lab microphone (`demo-tools/lab-say`: a PipeWire pipe
source and ydotool holding Super+V). Languages: `tests/lang-test.sh`
(what each speech model hears in English and Portuguese, word error rate
and time, through basalt-voiced's own transcribe path, and the refusal of
an English-only model) and `tests/lang-session.sh` (push to talk in
Portuguese on an English desktop, then English again, with the
confirmations clicked).

Results in the lab VM (10 vCPU, CPU only, utterances synthesized with
Piper, so a clean and regular voice; real voices and microphones do
worse):

| Speech | Model | Exact transcripts | Word error rate | Speech to text |
|---|---|---|---|---|
| pt-BR, 8 requests | ggml-base-q5_1 | 4 of 8 | 0.22 | 1.2 to 1.4 s |
| pt-BR, 8 requests | ggml-small-q5_1 | 5 of 8 | 0.11 | 3.7 to 4.0 s |
| en-US, 4 requests | ggml-base.en | 2 of 4 | 0.16 | 1.4 to 1.5 s |
| en-US, 4 requests | ggml-small-q5_1 | 2 of 4 | 0.16 | 3.7 to 3.8 s |
| automatic, 2 requests | ggml-small-q5_1 | 1 of 2 | 0.13 | 9.7 s |

In the session (push to talk on an English desktop), "use o tema lichen"
switched the theme and "deixe mais escuro" darkened it after the
confirmation, both through the fixed phrases (about 1.3 s from release
to the proposal with ggml-base-q5_1, 3.8 to 6 s with ggml-small-q5_1),
and the e-mail and file skills answered in Portuguese from the local
qwen3-1.7b model (summary 8 to 15 s, plan 2 to 5 s; English: 12 to 14 s
for a longer summary). The small model heard "escuro" as "escudo" or
"escuto" in 4 of 6 runs; twice the model then took it for a different
change (shown on the confirmation sheet, never applied by itself). The
base model heard it right in all 4 of its runs. The
Portuguese summaries were understandable but not always faithful: a
weekday translated wrong once and left in English once.

## Translation

Every string a person reads or hears goes through a catalog:
`internal/i18n` in Go (`G` for a whole sentence with placeholders, `N`
for plurals) and the `Tr` singleton in the QML of the voice card and the
Voice and assistant page (`Tr.t`, `Tr.n`, placeholders `%1`). English is
the reference and the message id; the catalogs are
`locale/<lang>.json`, installed in `/usr/share/basalt-shell/locale`, with
Brazilian Portuguese (`pt_BR.json`) as the first translation. The daemon
speaks in the person's answer language; the shell UI uses the catalog of
the session's language, which the daemon sends with its state.
`internal/i18n`'s test fails when a message of the Go sources or of a
`Tr` call has no translation in a catalog, when a translation's
placeholders differ, or when a catalog has an entry nothing uses. Other
QML still uses `qsTr` and English text until it moves to `Tr`.

## Push to talk and where the words go

The shell owns the microphone and decides where the words go when the key
goes down, and the card shows it while the person speaks:

- a text field has focus: dictation into it (as an input method, never
  synthetic key presses), previewed in the field and typed after Insert;
- no text field, the command bar is open, or the words start with
  "assistant": a request to the assistant;
- a password or PIN field: no dictation; the words go to the assistant.

Only one input method can be bound on a seat; with another one running
(IBus, Fcitx) dictation is off and every utterance goes to the assistant.
