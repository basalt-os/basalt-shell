#!/usr/bin/env python3
"""Record the synthetic English test set: every sentence in sentences.txt,
spoken by several voices of two TTS engines (Piper voices trained from
public-domain data, a multi-speaker Piper voice, Kokoro), clean and with
background noise at 15 dB SNR, as 16 kHz mono WAV files.

    make-set.py MODELS_DIR OUT_DIR [SENTENCES]

SENTENCES defaults to sentences.txt; a line may be "NAME | sentence" (the
names set, names.txt: the name the recognizer must spell).
Writes OUT_DIR/manifest.tsv: file, voice, noise, reference (and name).
"""
import json, os, subprocess, sys, wave
import numpy as np

models, out = sys.argv[1], sys.argv[2]
os.makedirs(out, exist_ok=True)
here = os.path.dirname(os.path.abspath(__file__))
src = sys.argv[3] if len(sys.argv) > 3 else os.path.join(here, "sentences.txt")
sentences, names = [], []
for l in open(src):
    l = l.strip()
    if not l or l.startswith("#"):
        continue
    n, _, t = l.partition(" | ") if " | " in l else ("", "", l)
    names.append(n.strip())
    sentences.append(t.strip())
piper = os.path.join(models, "piper/piper/piper")
rng = np.random.default_rng(7)

def resample(x, sr, to=16000):
    n = int(round(len(x) * to / sr))
    return np.interp(np.linspace(0, len(x) - 1, n), np.arange(len(x)), x).astype(np.float32)

def save(path, x):
    x = np.clip(x, -1, 1)
    with wave.open(path, "wb") as w:
        w.setnchannels(1); w.setsampwidth(2); w.setframerate(16000)
        w.writeframes((x * 32767).astype("<i2").tobytes())

def noisy(x, snr_db=15):
    # Pink-ish noise (filtered white) at the given SNR, plus a little hum.
    n = rng.standard_normal(len(x)).astype(np.float32)
    n = np.convolve(n, np.ones(8) / 8, mode="same")
    n += 0.3 * np.sin(2 * np.pi * 50 * np.arange(len(x)) / 16000)
    p = np.mean(x ** 2) + 1e-9
    n *= np.sqrt(p / (10 ** (snr_db / 10)) / (np.mean(n ** 2) + 1e-9))
    return x + n

def pad(x):
    # Half a second of silence before and after, as with a held key.
    z = np.zeros(8000, dtype=np.float32)
    return np.concatenate([z, x, z])

voices = []
for name, model, spk in [("piper-ljspeech-high", "en_US-ljspeech-high", None),
                         ("piper-kristin", "en_US-kristin-medium", None),
                         ("piper-norman", "en_US-norman-medium", None),
                         ("piper-john", "en_US-john-medium", None),
                         ("piper-libritts-r-p20", "en_US-libritts_r-medium", 20),
                         ("piper-libritts-r-p600", "en_US-libritts_r-medium", 600),
                         ("piper-alan-gb", "en_GB-alan-medium", None)]:
    voices.append(("piper", name, model, spk))
for v in ["af_heart", "am_michael", "bf_emma", "bm_george"]:
    voices.append(("kokoro", "kokoro-" + v, v, None))

kokoro = None
rows = []
for engine, name, model, spk in voices:
    for i, s in enumerate(sentences, 1):
        if engine == "piper":
            cfg = json.load(open(os.path.join(models, "piper", model + ".onnx.json")))
            sr = cfg["audio"]["sample_rate"]
            args = [piper, "--model", os.path.join(models, "piper", model + ".onnx"), "--output_raw", "--quiet"]
            if spk is not None:
                args += ["--speaker", str(spk)]
            raw = subprocess.run(args, input=s.encode(), capture_output=True, check=True).stdout
            x = np.frombuffer(raw, dtype="<i2").astype(np.float32) / 32768
        else:
            if kokoro is None:
                from kokoro_onnx import Kokoro
                kokoro = Kokoro(os.path.join(models, "kokoro/kokoro-v1.0.onnx"), os.path.join(models, "kokoro/voices-v1.0.bin"))
            lang = "en-gb" if model.startswith("b") else "en-us"
            x, sr = kokoro.create(s, voice=model, speed=1.0, lang=lang)
            x = np.asarray(x, dtype=np.float32)
        x = pad(resample(x, sr))
        for noise in ("clean", "noise15"):
            y = x if noise == "clean" else noisy(x)
            f = f"{name}-{noise}-{i:02d}.wav"
            save(os.path.join(out, f), y)
            rows.append((f, name, noise, s, names[i - 1]))
    print(name, "done", flush=True)
with open(os.path.join(out, "manifest.tsv"), "w") as m:
    m.write("file\tvoice\tnoise\treference\tname\n")
    for r in rows:
        m.write("\t".join(r) + "\n")
print(len(rows), "files")
