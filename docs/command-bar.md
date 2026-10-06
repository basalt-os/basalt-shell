# The command bar and the local model

The command bar ("Ask the system", Super+A) understands short requests
in English and Brazilian Portuguese and turns them into the same typed
actions the rest of the shell uses. Nothing runs until the person presses
Apply. With the Basalt OS local model installed (basalt-llm, see the
basalt-os documentation, `docs/local-model.md`), it understands more.

## How a request is understood

1. Fixed phrases (deterministic rules, `internal/intent/rules.go`). When
   they understand the whole request, they are used: exact and instant.
2. Otherwise, the local model, when the system assistant's translator is
   on (`[translator] enabled = yes` in `/etc/basalt/assistant.conf`, the
   same service `basalt ask` uses). The model's answer is constrained by a
   JSON schema (llama.cpp turns it into a grammar) to a closed set of
   desktop intents, each with only its own fields:

   | Intents | Fields |
   |---|---|
   | dark_mode, light_mode, toggle_mode, darker, lighter, rounder_corners, sharper_corners, square_corners, bigger_text, smaller_text, more_spacing, less_spacing, reduce_motion, full_motion, panel_top, panel_bottom, shadows_on, shadows_off, blur_on, blur_off, theme_reset, spoken_answers_off, spoken_answers_on, lock_screen, log_out, suspend, restart, power_off | none |
   | accent | color, from a list |
   | use_theme | theme, from the installed themes |
   | arrange | layout, from a list |
   | switch_workspace | workspace number |
   | move_window | window, workspace |
   | close_window, focus_window, float_window, tile_window | window |
   | open_app | app |
   | open_settings | page, from a list |
   | system, clarify, none | none |

   The shell then checks the answer against the request: an application
   or window name must be made of words the person wrote (one typo
   allowed), a workspace number must appear in the request (digits or
   words), so a model that invents a target gets "please say which one".
   Each intent becomes a canonical phrase that goes through the same
   rules, so the model adds languages and paraphrases, never actions.
3. When the model is unavailable or unsure, whatever the fixed phrases
   understood is used; else the bar asks for detail or says the request
   is outside what the desktop does.
4. System questions ("why nginx", "disk", "snapshots", "selinux denials")
   go to the system assistant. When the model says a request is about the
   system, the assistant's own translator picks the command (`basalt ask
   --dry-run`, then the read command through the read helper); the
   assistant's report and proposal are shown, and applying it goes
   through the assistant's own confirmation (`basalt apply --confirm`).

The model service's socket is open only to the assistant and to
administrators. The shell reaches it through its read helper
(`assistant-read translate`, run with pkexec; a polkit rule lets local
administrators in an active session use it without a password): the
helper accepts one chat completions body on stdin, at most 64 KiB, and
forwards it to the Unix socket configured for the translator, never to a
network endpoint. The model has no power: its answer is validated by the
shell and becomes a proposal.

The fixed phrases cover the main requests in Brazilian Portuguese as
speech recognition writes them ("deixe mais escuro", "modo escuro", "use
o tema lichen" and the Portuguese names of the themes, "organize as
janelas", "aumente o texto", "abra as configurações", "pare de falar as
respostas"); they map to the same English intents and actions. "Stop
speaking answers" and "speak answers" (in Portuguese "pare de falar as
respostas" and "fale as respostas") turn the person's spoken answers off
and on (the action `voice.answers.set`, confirmed like the other changes;
see `docs/voice.md`, Languages). When the person's answer language is
not English, the translator's prompt gets one line asking for answers in
that language while every identifier stays English (see `docs/voice.md`,
Model); its schema and the checks above are the same in every language.

Every request is in the activity log with how it was understood: the
backend (rules or model), the model's answer, the time it took, the
phrases and calls, and what grounding dropped.

## Measured (lab VM, 4 vCPU, CPU only, 2026-10-04)

32 requests (`lab/demo/desktop-requests.tsv`: English and Portuguese,
paraphrases, system questions, requests outside the desktop), scored by
`lab/demo/score-translator.py` on the outcome (the right actions, the
right direction of a change, system, or refused):

| | fixed phrases | model alone | model, then phrases | phrases, then model (shipped) | model p50 |
|---|---|---|---|---|---|
| basalt-translator-0.6b-q8_0 (fine-tuned for `basalt ask`, the default) | 22/32 (69%) | 24/32 (75%) | 26/32 (81%) | 29/32 (91%) | 1.0 s |
| qwen3-1.7b-q8_0 (untuned) | 22/32 | 24/32 | 25/32 | 27/32 (84%) | 2.0 s |
| 0.6B after fixing 4 rules the evaluation exposed (same set, so in-sample) | 26/32 | 24/32 | 27/32 | 31/32 (97%) | 1.2 s |

Times include pkexec and the helper. The fine-tuned 0.6B was trained on
the assistant's system intents, not on desktop requests; its typical
mistakes were picking a neighbour intent (an accent color became a theme
switch, "open firefox" became nothing, "abre o editor de texto" became
settings). Next: add desktop request/intent pairs to the translator's
training set (the schema above is the target format) and keep this set as
the desktop part of the shared evaluation suite.
