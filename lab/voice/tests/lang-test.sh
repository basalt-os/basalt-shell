#!/bin/bash
# Speech languages in the voice lab VM (session user): what each speech
# model hears in English and in Brazilian Portuguese, how long it takes,
# and that an English-only model refuses Portuguese. The utterances are
# synthesized with Piper (a pt_BR voice kept in the lab directory only, as
# test input: it is never part of Basalt OS) and resampled to 16 kHz, then
# run through basalt-voiced's own transcribe path (the same arguments,
# language flag, prompt and policy checks as push to talk).
#   lang-test.sh OUT.jsonl
# Settings: LAB_PIPER_DIR (default /opt/basalt-voice-lab/models/piper),
# MODELS_PT (default "ggml-base-q5_1 ggml-small-q5_1"), MODELS_EN
# (default "ggml-base.en ggml-small-q5_1").
set -u
out=${1:?out.jsonl}
piper_dir=${LAB_PIPER_DIR:-/opt/basalt-voice-lab/models/piper}
piper=/usr/libexec/basalt-voice/piper/piper
work=$(mktemp -d)
# The voice domain reads only its own runtime directory (no /tmp, no
# home): the test input goes there, as the recording would.
lab_wav=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/basalt-voice/lab-utterance.wav
trap 'rm -rf "$work" "$lab_wav"' EXIT
set -a; . /etc/basalt/voice.conf; set +a

synth() { # synth VOICE TEXT FILE: 16 kHz mono WAV
  echo "$2" | LD_LIBRARY_PATH=$(dirname "$piper") "$piper" --model "$piper_dir/$1.onnx" --output_raw --quiet >"$work/raw"
  python3 - "$work/raw" "$3" <<'PY'
import array, struct, sys
d = array.array("h"); d.frombytes(open(sys.argv[1], "rb").read())
r = 22050 / 16000; n = int(len(d) / r); o = array.array("h")
for i in range(n):
    x = i * r; j = int(x); f = x - j
    a = d[j] if j < len(d) else 0; b = d[j + 1] if j + 1 < len(d) else a
    o.append(int(a + (b - a) * f))
pcm = b"\0" * 8000 + o.tobytes() + b"\0" * 6400
h = b"RIFF" + struct.pack("<I", 36 + len(pcm)) + b"WAVEfmt " + struct.pack("<IHHIIHH", 16, 1, 1, 16000, 32000, 2, 16) + b"data" + struct.pack("<I", len(pcm))
open(sys.argv[2], "wb").write(h + pcm)
PY
}

run() { # run LANG MODEL VOICE TEXT
  local f=$lab_wav
  synth "$3" "$4" "$f"
  local t0; t0=$(date +%s%N)
  local res
  # The shell adds the installed themes' names to each request's prompt.
  local names="Themes: Basalt, Lichen, Tide."
  case $1 in pt*) names="Temas: Basalt, Lichen, Tide." ;; esac
  res=$(BASALT_VOICE_TEST_NAMES=$names BASALT_VOICE_TEST_LANG=$1 BASALT_VOICE_TEST_MODEL=$2 basalt-voiced transcribe "$f" 2>&1)
  local rc=$?
  local ms; ms=$((($(date +%s%N) - t0) / 1000000))
  python3 - "$1" "$2" "$4" "$rc" "$ms" "$res" <<'PY' | tee -a "$out"
import json, re, sys, unicodedata
lang, model, said, rc, ms, res = sys.argv[1:7]
def norm(s):
    s = unicodedata.normalize("NFC", s.lower())
    return re.sub(r"[^\w ]+", "", s).split()
heard, err = "", ""
try:
    heard = json.loads(res).get("text", "")
except Exception:
    err = res.strip().splitlines()[-1] if res.strip() else "failed"
a, b = norm(said), norm(heard)
# word error rate (Levenshtein over words)
d = list(range(len(b) + 1))
for i, x in enumerate(a, 1):
    p, d[0] = d[0], i
    for j, y in enumerate(b, 1):
        p, d[j] = d[j], min(d[j] + 1, d[j - 1] + 1, p + (x != y))
wer = round(d[len(b)] / max(1, len(a)), 3)
print(json.dumps({"lang": lang, "model": model, "said": said, "heard": heard, "exact": a == b, "wer": wer if not err else None,
                  "ms": int(ms), "error": err}, ensure_ascii=False))
PY
}

: >"$out"
pt=(
  "Deixe mais escuro."
  "Use o tema lichen."
  "Organize as janelas lado a lado."
  "Encontre o PDF que o banco mandou no mês passado."
  "O que a Ana disse no último e-mail?"
  "Resuma a página de notícias."
  "Assistente, abra o resultado dois."
  "Aumente o texto."
)
en=(
  "Make it darker."
  "Use the lichen theme."
  "Find the PDF the bank sent last month."
  "What did Ana say in her last email?"
)
for m in ${MODELS_PT:-ggml-base-q5_1 ggml-small-q5_1}; do
  for s in "${pt[@]}"; do run pt-BR "$m" pt_BR-faber-medium "$s"; done
done
for m in ${MODELS_EN:-ggml-base.en ggml-small-q5_1}; do
  for s in "${en[@]}"; do run en-US "$m" en_US-kristin-medium "$s"; done
done
# Refusal: an English-only model for Portuguese.
run pt-BR ggml-base.en pt_BR-faber-medium "Deixe mais escuro."
# Automatic detection with a multilingual model.
run auto ggml-small-q5_1 pt_BR-faber-medium "Use o tema lichen."
run auto ggml-small-q5_1 en_US-kristin-medium "Make it darker."
python3 - "$out" <<'PY'
import json, sys, collections
rows = [json.loads(l) for l in open(sys.argv[1])]
g = collections.defaultdict(list)
for r in rows:
    if r["wer"] is not None:
        g[(r["lang"], r["model"])].append(r)
for (lang, model), rs in sorted(g.items()):
    n = len(rs)
    print(f"{lang:6} {model:18} n={n} exact={sum(r['exact'] for r in rs)}/{n} mean WER={sum(r['wer'] for r in rs)/n:.3f} "
          f"mean ms={sum(r['ms'] for r in rs)//n} max ms={max(r['ms'] for r in rs)}")
for r in rows:
    if r["error"]:
        print("refused:", r["lang"], r["model"], "->", r["error"])
PY
