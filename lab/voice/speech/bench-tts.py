#!/usr/bin/env python3
"""Text-to-speech latency on the VM: time to synthesize the first sentence
of typical spoken answers (what the person waits for), and the real-time
factor, for Piper voices (warm process, JSON lines, as basalt-voiced runs
it) and Kokoro (kokoro-onnx, fp32 and int8).

    bench-tts.py OUT_JSONL
"""
import json, os, statistics, subprocess, sys, tempfile, time, wave

L = "/opt/basalt-voice-lab"
answers = [
    "I found 3 files.",
    "The best match is account statement September 2026, created on 12 September, in Documents.",
    "Ana asked if you are still on for lunch on Thursday at 12:30.",
    "Warning: this page tries to instruct the assistant. I ignored it.",
    "The page says heavy rain is expected on Friday from two in the afternoon.",
    "I need your permission to read your documents. Please confirm on the screen.",
]
out = open(sys.argv[1], "w")

def piper(voice):
    d = tempfile.mkdtemp()
    p = subprocess.Popen([L + "/models/piper/piper/piper", "--model", voice, "--json-input", "--output_dir", d, "--quiet"],
                         stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True,
                         env=dict(os.environ, LD_LIBRARY_PATH=L + "/models/piper/piper"))
    times, rtf = [], []
    for i, s in enumerate(answers * 2):
        f = os.path.join(d, "s%d.wav" % i)
        t = time.perf_counter()
        p.stdin.write(json.dumps({"text": s, "output_file": f}) + "\n"); p.stdin.flush()
        p.stdout.readline()
        dt = time.perf_counter() - t
        with wave.open(f) as w:
            dur = w.getnframes() / w.getframerate()
        if i >= len(answers) // 2:  # skip the first (warm-up) round partly
            times.append(dt); rtf.append(dt / dur)
    p.stdin.close(); p.wait()
    return times, rtf

def kokoro(model):
    from kokoro_onnx import Kokoro
    t = time.perf_counter()
    k = Kokoro(model, L + "/models/kokoro/voices-v1.0.bin")
    load = time.perf_counter() - t
    times, rtf = [], []
    for i, s in enumerate(answers * 2):
        t = time.perf_counter()
        x, sr = k.create(s, voice="af_heart", speed=1.0, lang="en-us")
        dt = time.perf_counter() - t
        if i >= len(answers) // 2:
            times.append(dt); rtf.append(dt / (len(x) / sr))
    return times, rtf, load

for name, v in [("piper-ljspeech-medium", "en_US-ljspeech-medium"), ("piper-ljspeech-high", "en_US-ljspeech-high"),
                ("piper-kristin-medium", "en_US-kristin-medium"), ("piper-norman-medium", "en_US-norman-medium")]:
    t, r = piper(L + "/models/piper/" + v + ".onnx")
    row = {"engine": name, "p50_ms": round(1000 * statistics.median(t)), "max_ms": round(1000 * max(t)), "rtf": round(statistics.median(r), 3)}
    print(json.dumps(row)); out.write(json.dumps(row) + "\n")
for name, m in [("kokoro-int8", "kokoro-v1.0.int8.onnx"), ("kokoro-fp32", "kokoro-v1.0.onnx")]:
    t, r, load = kokoro(L + "/models/kokoro/" + m)
    row = {"engine": name, "p50_ms": round(1000 * statistics.median(t)), "max_ms": round(1000 * max(t)), "rtf": round(statistics.median(r), 3), "load_s": round(load, 1)}
    print(json.dumps(row)); out.write(json.dumps(row) + "\n")
