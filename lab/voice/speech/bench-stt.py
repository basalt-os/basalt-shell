#!/usr/bin/env python3
"""Speech-to-text latency and accuracy on the synthetic English set.

    bench-stt.py SET_DIR OUT_JSONL ENGINE MODEL [--limit N] [--threads 4] [--beam 1]

ENGINE:
  cli      whisper.cpp whisper-cli, one process per utterance (cold: the
           model is loaded every time, as a per-request process would)
  server   whisper.cpp whisper-server, warm, HTTP on 127.0.0.1
  fw       faster-whisper (CTranslate2, int8), warm, in this process
All with Silero VAD (whisper.cpp --vad, faster-whisper vad_filter).
Prints a summary line; writes one JSON line per file.
"""
import argparse, json, os, re, statistics, subprocess, sys, time, urllib.request, uuid

ap = argparse.ArgumentParser()
ap.add_argument("set"); ap.add_argument("out"); ap.add_argument("engine"); ap.add_argument("model")
ap.add_argument("--limit", type=int, default=0); ap.add_argument("--threads", type=int, default=4)
ap.add_argument("--beam", type=int, default=1); ap.add_argument("--every", type=int, default=1)
ap.add_argument("--bin", default="/opt/basalt-voice-lab/whisper/bin")
ap.add_argument("--vad", default="/opt/basalt-voice-lab/models/whisper/ggml-silero-v5.1.2.bin")
ap.add_argument("--gpu", action="store_true")
ap.add_argument("--ac", type=int, default=0, help="whisper.cpp audio context (0: full 30 s window)")
ap.add_argument("--prompt", default="", help="whisper.cpp initial prompt")
a = ap.parse_args()

rows = [l.rstrip("\n").split("\t") for l in open(os.path.join(a.set, "manifest.tsv"))][1:]
rows = rows[::a.every]
if a.limit:
    rows = rows[:a.limit]

NUM = {"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4", "five": "5", "six": "6", "seven": "7",
       "eight": "8", "nine": "9", "ten": "10", "second": "2nd", "first": "1st"}
def norm(s):
    s = s.lower().replace("e-mail", "email").replace("’", "'")
    s = re.sub(r"(?<=\d),(?=\d)", "", s)
    s = re.sub(r"[^a-z0-9' ]+", " ", s)
    return " ".join(NUM.get(w, w) for w in s.split())

def wer(ref, hyp):
    r, h = norm(ref).split(), norm(hyp).split()
    d = list(range(len(h) + 1))
    for i in range(1, len(r) + 1):
        p, d[0] = d[0], i
        for j in range(1, len(h) + 1):
            p, d[j] = d[j], min(d[j] + 1, d[j - 1] + 1, p + (r[i - 1] != h[j - 1]))
    return d[len(h)], len(r)

def audio_ms(path):
    import wave
    with wave.open(path) as w:
        return 1000 * w.getnframes() / w.getframerate()

model_path = a.model
transcribe = None
srv = None
if a.engine == "cli":
    def transcribe(path):
        args = [os.path.join(a.bin, "whisper-cli"), "-m", model_path, "-f", path, "-l", "en", "-t", str(a.threads),
                "-nt", "-np", "-bs", str(a.beam), "-bo", str(max(a.beam, 1)), "--vad", "-vm", a.vad]
        if not a.gpu:
            args.append("-ng")
        if a.ac:
            args += ["-ac", str(a.ac)]
        if a.prompt:
            args += ["--prompt", a.prompt]
        p = subprocess.run(args, capture_output=True, text=True)
        return p.stdout.strip()
elif a.engine == "server":
    port = 18000 + os.getpid() % 1000
    args = [os.path.join(a.bin, "whisper-server"), "-m", model_path, "--host", "127.0.0.1", "--port", str(port),
            "-t", str(a.threads), "-l", "en", "-bs", str(a.beam), "-bo", str(max(a.beam, 1)), "--vad", "-vm", a.vad]
    if not a.gpu:
        args.append("-ng")
    srv = subprocess.Popen(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(120):
        try:
            urllib.request.urlopen(f"http://127.0.0.1:{port}/", timeout=1); break
        except Exception:
            time.sleep(0.5)
    def transcribe(path):
        b = "----" + uuid.uuid4().hex
        data = open(path, "rb").read()
        body = (f"--{b}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.wav\"\r\nContent-Type: audio/wav\r\n\r\n").encode() + data + \
               (f"\r\n--{b}\r\nContent-Disposition: form-data; name=\"response_format\"\r\n\r\njson\r\n--{b}--\r\n").encode()
        req = urllib.request.Request(f"http://127.0.0.1:{port}/inference", data=body, headers={"Content-Type": "multipart/form-data; boundary=" + b})
        return json.loads(urllib.request.urlopen(req, timeout=120).read()).get("text", "").strip()
elif a.engine == "fw":
    from faster_whisper import WhisperModel
    t0 = time.time()
    m = WhisperModel(model_path, device="cuda" if a.gpu else "cpu", compute_type="int8" if not a.gpu else "float16", cpu_threads=a.threads)
    print(f"load {time.time() - t0:.2f}s", file=sys.stderr)
    import numpy as np, wave
    def transcribe(path):
        # Decoded here (16 kHz mono PCM): the PyAV in the lab venv rejects
        # faster-whisper's decoder options.
        with wave.open(path) as w:
            audio = np.frombuffer(w.readframes(w.getnframes()), dtype="<i2").astype(np.float32) / 32768
        segs, _ = m.transcribe(audio, language="en", beam_size=a.beam, vad_filter=True, condition_on_previous_text=False)
        return " ".join(s.text.strip() for s in segs)

errs = words = 0
names_n = [0, 0]
lat = []
by = {}
with open(a.out, "w") as out:
    for row in rows:
        f, voice, noise, ref = row[:4]
        name = row[4] if len(row) > 4 else ""
        p = os.path.join(a.set, f)
        t0 = time.perf_counter()
        hyp = transcribe(p)
        ms = 1000 * (time.perf_counter() - t0)
        e, n = wer(ref, hyp)
        errs += e; words += n; lat.append(ms)
        k = by.setdefault(noise, [0, 0]); k[0] += e; k[1] += n
        name_ok = None
        if name:
            # The name is right when every word of it is spelled as such.
            hw = set(norm(hyp).split())
            name_ok = all(w in hw for w in norm(name).split())
            names_n[0] += 1
            names_n[1] += 1 if name_ok else 0
        out.write(json.dumps({"file": f, "voice": voice, "noise": noise, "ref": ref, "hyp": hyp, "ms": round(ms), "audio_ms": round(audio_ms(p)), "errors": e, "words": n, "name": name, "name_ok": name_ok}) + "\n")
if srv:
    srv.terminate()
lat.sort()
summary = {"engine": a.engine, "ac": a.ac, "model": os.path.basename(model_path.rstrip("/")), "beam": a.beam, "threads": a.threads, "gpu": a.gpu, "n": len(rows),
           "wer": round(100 * errs / max(words, 1), 2), "wer_clean": round(100 * by.get("clean", [0, 1])[0] / max(by.get("clean", [0, 1])[1], 1), 2),
           "wer_noise15": round(100 * by.get("noise15", [0, 1])[0] / max(by.get("noise15", [0, 1])[1], 1), 2),
           "p50_ms": round(statistics.median(lat)), "p90_ms": round(lat[int(0.9 * (len(lat) - 1))]), "mean_ms": round(statistics.mean(lat)),
           "prompt": bool(a.prompt)}
if names_n[0]:
    summary["names_ok"] = round(100 * names_n[1] / names_n[0], 1)
print(json.dumps(summary))
