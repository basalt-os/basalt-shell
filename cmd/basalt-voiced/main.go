// Command basalt-voiced is the desktop's voice service: push to talk,
// local speech to text and local text to speech. With the Basalt policy
// it runs in its own SELinux domain (basalt_voice_t): it may use PipeWire
// and run its speech programs, it has no network and cannot read the
// person's files.
//
// The microphone is open only between "listen" and "stop", which only the
// shell daemon may send (peer checked by SELinux context): the shell
// sends them while the person holds the push-to-talk key or the panel's
// microphone button. A hold longer than the limit (30 s) is cut. The
// recording stays in memory; the speech-to-text program reads it from a
// private file in the runtime directory (memory, not disk) that is
// removed right after. Nothing is kept.
//
// Settings (environment, from /etc/basalt/voice.conf or the user unit):
//
//	BASALT_VOICE_STT_BIN    whisper-cli (whisper.cpp)
//	BASALT_VOICE_STT_MODEL  ggml model (default base.en)
//	BASALT_VOICE_VAD_MODEL  Silero VAD model for whisper.cpp
//	BASALT_VOICE_THREADS    threads for speech to text (default 4)
//	BASALT_VOICE_TTS_BIN    piper (or a program with the same JSON-lines protocol)
//	BASALT_VOICE_TTS_MODEL  voice model
//	BASALT_VOICE_MAX_HOLD   seconds (default 30)
//	BASALT_VOICE_PEER       selinux (default when the policy is loaded), exe, insecure
//	BASALT_VOICE_LANGUAGE   default speech language: auto or a tag (default en)
//	BASALT_VOICE_ALLOWED_MODELS, BASALT_VOICE_MAX_MODEL_MB, BASALT_VOICE_MODEL_DIRS
//	                        what a person may choose (see internal/voice.System)
//
// Per person: the shell daemon sends the person's speech language and
// speech model with each "stop", and their voice with each "speak" (from
// their settings file, which this service never reads: it has no access
// to home files). Every choice is checked here against the system policy
// above, on every request; a model outside it, or an English-only model
// for another language, is refused with a coded error the shell shows on
// the voice card. Changing the person's settings needs no restart; a
// change of /etc/basalt/voice.conf needs a restart of the service.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/basalt-os/basalt-shell/internal/harden"
	"github.com/basalt-os/basalt-shell/internal/voice"
)

var version = "0.1.0-spike"

type config struct {
	sttBin, sttModel, vadModel string
	threads                    int
	ttsBin, ttsModel           string
	ttsArgs                    []string
	maxHold                    time.Duration
	peer                       string
	prompt                     string
	runDir                     string
	rate                       int
	pwRecord, pwPlay           string
	sys                        voice.System
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func load() config {
	c := config{
		sttBin:   env("BASALT_VOICE_STT_BIN", "/usr/libexec/basalt-voice/whisper-cli"),
		sttModel: env("BASALT_VOICE_STT_MODEL", "/usr/share/basalt-voice/models/ggml-base.en.bin"),
		vadModel: env("BASALT_VOICE_VAD_MODEL", "/usr/share/basalt-voice/models/ggml-silero-v5.1.2.bin"),
		ttsBin:   env("BASALT_VOICE_TTS_BIN", "/usr/libexec/basalt-voice/piper/piper"),
		ttsModel: env("BASALT_VOICE_TTS_MODEL", "/usr/share/basalt-voice/voices/en_US-ljspeech-medium.onnx"),
		peer:     env("BASALT_VOICE_PEER", "auto"),
		prompt:   env("BASALT_VOICE_PROMPT", "Find the PDF. Summarize my email, my inbox, the web page. Reply to the email. Open result 2. Documents, Downloads, Desktop."),
		rate:     16000,
		// Absolute paths: the voice domain may run these programs and
		// no other (no PATH search through the person's folders).
		pwRecord: env("BASALT_VOICE_PW_RECORD", "/usr/bin/pw-record"),
		pwPlay:   env("BASALT_VOICE_PW_PLAY", "/usr/bin/pw-play"),
	}
	c.threads, _ = strconv.Atoi(env("BASALT_VOICE_THREADS", "4"))
	hold, _ := strconv.Atoi(env("BASALT_VOICE_MAX_HOLD", "30"))
	c.maxHold = time.Duration(hold) * time.Second
	if a := os.Getenv("BASALT_VOICE_TTS_ARGS"); a != "" {
		c.ttsArgs = strings.Fields(a)
	}
	rd := os.Getenv("XDG_RUNTIME_DIR")
	if rd == "" {
		rd = os.TempDir()
	}
	c.runDir = filepath.Join(rd, "basalt-voice")
	c.sys = voice.SystemFromEnv(os.Getenv)
	c.sys.STTModel, c.sys.TTSModel = c.sttModel, c.ttsModel
	return c
}

// ------------------------------------------------------------ service

type service struct {
	cfg config

	mu       sync.Mutex
	state    string
	capture  *exec.Cmd
	pcm      *bytes.Buffer
	capDone  chan struct{}
	holdTmr  *time.Timer
	started  time.Time
	tts      *tts
	ttsModel string // the voice the warm Piper process speaks with
	player   *exec.Cmd
	speakGen int
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("basalt-voiced: ")
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("basalt-voiced", version)
		return
	}
	// The unit sets NoNewPrivileges too; this covers a start by hand.
	if err := harden.NoNewPrivs(); err != nil {
		log.Fatalf("no_new_privs: %v", err)
	}
	cfg := load()
	if len(os.Args) > 2 && os.Args[1] == "transcribe" {
		// basalt-voiced transcribe FILE.wav: the same speech to text, for tests.
		s := &service{cfg: cfg}
		b, err := os.ReadFile(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
		t, err := s.transcribe(context.Background(), b[44:], os.Getenv("BASALT_VOICE_TEST_LANG"), os.Getenv("BASALT_VOICE_TEST_MODEL"))
		if err != nil {
			log.Fatal(err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(t)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// A fresh runtime directory: it holds only this service's socket and
	// transient files, and creating it gives it the service's SELinux type.
	_ = os.RemoveAll(cfg.runDir)
	if err := os.MkdirAll(cfg.runDir, 0o700); err != nil {
		log.Fatal(err)
	}
	_ = os.Chmod(cfg.runDir, 0o700)
	sock := filepath.Join(cfg.runDir, "voice.sock")
	_ = os.Remove(sock)
	l, err := net.Listen("unix", sock)
	if err != nil {
		log.Fatal(err)
	}
	_ = os.Chmod(sock, 0o600)
	mode := peerMode(cfg.peer)
	s := &service{cfg: cfg, state: "idle"}
	log.Printf("%s listening on %s (peer check %s; stt %s; tts %s %s; max hold %s)", version, sock, mode,
		filepath.Base(cfg.sttModel), filepath.Base(cfg.ttsBin), filepath.Base(cfg.ttsModel), cfg.maxHold)
	go func() { <-ctx.Done(); l.Close(); s.shutdown() }()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Fatal(err)
		}
		go s.serve(ctx, c.(*net.UnixConn), mode)
	}
}

// ------------------------------------------------------------ peers

const soPeerSec = 31

func peerOf(c *net.UnixConn) (uid int, typ, exe string, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, "", "", err
	}
	var pid int
	cerr := raw.Control(func(fd uintptr) {
		cred, e := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if e != nil {
			err = e
			return
		}
		uid, pid = int(cred.Uid), int(cred.Pid)
		buf := make([]byte, 256)
		n := uint32(len(buf))
		_, _, e2 := syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd, syscall.SOL_SOCKET, soPeerSec,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), 0)
		if e2 == 0 && n > 0 && int(n) <= len(buf) {
			parts := strings.SplitN(string(bytes.TrimRight(buf[:n], "\x00")), ":", 4)
			if len(parts) >= 3 {
				typ = parts[2]
			}
		}
	})
	if cerr != nil {
		return 0, "", "", cerr
	}
	exe, _ = os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	// A daemon whose program was updated on disk while it runs reads as
	// "/usr/bin/basalt-shelld (deleted)": still the same program.
	exe = strings.TrimSuffix(exe, " (deleted)")
	return uid, typ, exe, err
}

func peerMode(want string) string {
	if want == "exe" || want == "insecure" || want == "selinux" {
		return want
	}
	f, err := os.OpenFile("/sys/fs/selinux/context", os.O_RDWR, 0)
	if err == nil {
		defer f.Close()
		if _, err := f.Write([]byte("system_u:object_r:basalt_voice_exec_t:s0\x00")); err == nil {
			return "selinux"
		}
	}
	return "exe"
}

// allowed: only the shell daemon may drive the voice service.
func allowed(mode, typ, exe string) error {
	switch mode {
	case "insecure":
		return nil
	case "selinux":
		if typ == "basalt_shell_t" && filepath.Base(exe) == "basalt-shelld" {
			return nil
		}
		return fmt.Errorf("domain %q (%s) is not the shell daemon", typ, filepath.Base(exe))
	default:
		if filepath.Base(exe) == "basalt-shelld" {
			return nil
		}
		return fmt.Errorf("program %q is not the shell daemon", filepath.Base(exe))
	}
}

func (s *service) serve(ctx context.Context, c *net.UnixConn, mode string) {
	defer c.Close()
	uid, typ, exe, err := peerOf(c)
	if err != nil || uid != os.Getuid() {
		return
	}
	w := json.NewEncoder(c)
	if err := allowed(mode, typ, exe); err != nil {
		log.Printf("refused a client: %v", err)
		_ = w.Encode(voice.Reply{Error: "refused: " + err.Error()})
		return
	}
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req voice.Request
		if json.Unmarshal(line, &req) != nil {
			_ = w.Encode(voice.Reply{Error: "bad request"})
			continue
		}
		rep := s.handle(ctx, req)
		rep.ID = req.ID
		_ = w.Encode(rep)
	}
}

func (s *service) handle(ctx context.Context, req voice.Request) voice.Reply {
	switch req.Op {
	case "listen":
		if err := s.listen(); err != nil {
			return voice.Reply{Error: err.Error()}
		}
		return voice.Reply{OK: true}
	case "stop":
		t, err := s.stopListening(ctx, extraPrompt(req.Prompt), req.Dictation, req.Lang, req.Model)
		if err != nil {
			return voice.Reply{Error: err.Error(), Code: voice.ErrorCode(err), Transcript: t}
		}
		return voice.Reply{OK: true, Transcript: t}
	case "cancel":
		s.cancel()
		return voice.Reply{OK: true}
	case "speak":
		sp, err := s.speak(ctx, req.Text, req.Voice)
		if err != nil {
			return voice.Reply{Error: err.Error(), Code: voice.ErrorCode(err)}
		}
		return voice.Reply{OK: true, Spoken: sp}
	case "hush":
		s.hush()
		return voice.Reply{OK: true}
	case "models":
		// The installed models and voices with the policy's verdict, for
		// the person's settings (names and sizes only).
		m := s.cfg.sys.List()
		return voice.Reply{OK: true, Models: &m}
	case "status":
		s.mu.Lock()
		st := &voice.Status{State: s.state, STT: filepath.Base(s.cfg.sttModel), TTS: filepath.Base(s.cfg.ttsBin),
			Voice: filepath.Base(s.cfg.ttsModel), Mic: s.capture != nil, MaxHold: int(s.cfg.maxHold / time.Second)}
		s.mu.Unlock()
		return voice.Reply{OK: true, Status: st}
	}
	return voice.Reply{Error: "unknown op " + strconv.Quote(req.Op)}
}

// ------------------------------------------------------------ capture

func (s *service) listen() error {
	s.hush()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.capture != nil {
		return nil // already listening (key repeat)
	}
	// A raw 16 kHz mono stream from the default source, named so the
	// panel's privacy indicator says who records.
	cmd := exec.Command(s.cfg.pwRecord, "--raw", "--rate", strconv.Itoa(s.cfg.rate), "--channels", "1", "--format", "s16",
		"--media-category", "Capture", "--media-role", "Communication",
		"-P", `{ application.name = "Basalt voice" media.name = "Push to talk" node.description = "Basalt push to talk" }`, "-")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("microphone: %w", err)
	}
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		// At most max hold seconds of audio are kept.
		lim := int64(s.cfg.rate*2) * int64(s.cfg.maxHold/time.Second+1)
		_, _ = io.Copy(buf, io.LimitReader(out, lim))
		_, _ = io.Copy(io.Discard, out)
		close(done)
	}()
	s.capture, s.pcm, s.capDone, s.started, s.state = cmd, buf, done, time.Now(), "listening"
	// The hold limit closes the microphone by itself.
	s.holdTmr = time.AfterFunc(s.cfg.maxHold, func() {
		s.mu.Lock()
		c := s.capture
		s.mu.Unlock()
		if c != nil && c.Process != nil {
			log.Printf("hold limit reached: microphone closed")
			_ = c.Process.Signal(syscall.SIGINT)
		}
	})
	log.Printf("microphone open")
	return nil
}

func (s *service) closeMic() (*bytes.Buffer, time.Duration, time.Duration) {
	s.mu.Lock()
	cmd, buf, done, started := s.capture, s.pcm, s.capDone, s.started
	if s.holdTmr != nil {
		s.holdTmr.Stop()
	}
	s.capture = nil
	s.mu.Unlock()
	if cmd == nil {
		return nil, 0, 0
	}
	held := time.Since(started)
	t := time.Now()
	_ = cmd.Process.Signal(syscall.SIGINT)
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		_ = cmd.Process.Kill()
		<-done
	}
	_ = cmd.Wait()
	log.Printf("microphone closed after %s", held.Round(10*time.Millisecond))
	return buf, held, time.Since(t)
}

func (s *service) cancel() {
	buf, _, _ := s.closeMic()
	if buf != nil {
		buf.Reset()
	}
	s.setState("idle")
}

func (s *service) setState(st string) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

func (s *service) stopListening(ctx context.Context, extra string, dictation bool, lang, model string) (*voice.Transcript, error) {
	buf, held, capMS := s.closeMic()
	if buf == nil {
		return nil, errors.New("not listening")
	}
	defer buf.Reset()
	// The person's choices, checked against the policy before anything
	// runs (the recording is dropped on a refusal).
	path, err := s.cfg.sys.Find(model, "stt")
	if err != nil {
		return nil, err
	}
	if lang == "" {
		lang = s.cfg.sys.Language
	}
	wl, err := voice.WhisperLanguage(lang, path)
	if err != nil {
		return nil, err
	}
	s.setState("transcribing")
	defer s.setState("idle")
	pcm := buf.Bytes()
	t := &voice.Transcript{AudioMS: held.Milliseconds(), CaptureMS: capMS.Milliseconds(), Model: filepath.Base(path), Lang: wl}
	t.Level = rms(pcm)
	if len(pcm) < s.cfg.rate*2*3/10 { // under 0.3 s
		return t, nil
	}
	tr, err := s.transcribeWith(ctx, pcm, extra, dictation, path, wl)
	if tr != nil {
		tr.AudioMS, tr.CaptureMS, tr.Level = t.AudioMS, t.CaptureMS, t.Level
	}
	return tr, err
}

func rms(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
		sum += v * v
	}
	return math.Round(math.Sqrt(sum/float64(n))*10000) / 10000
}

func wav(pcm []byte, rate int) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

var reNoise = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)

// transcribe runs whisper.cpp with Silero VAD on one utterance (the
// transcribe command for tests: language and model by name, checked
// like a request).
func (s *service) transcribe(ctx context.Context, pcm []byte, lang, model string) (*voice.Transcript, error) {
	path, err := s.cfg.sys.Find(model, "stt")
	if err != nil {
		return nil, err
	}
	if lang == "" {
		lang = s.cfg.sys.Language
	}
	wl, err := voice.WhisperLanguage(lang, path)
	if err != nil {
		return nil, err
	}
	return s.transcribeWith(ctx, pcm, os.Getenv("BASALT_VOICE_TEST_NAMES"), os.Getenv("BASALT_VOICE_TEST_DICTATION") == "1", path, wl)
}

// requestPrompts bias the recognizer toward the words of the desktop's
// requests, per language (Whisper heard "PDF" as "PD of" in the lab
// without it). A language without one gets no request prompt.
var requestPrompts = map[string]string{
	"en": "Find the PDF. Summarize my email, my inbox, the web page. Reply to the email. Open result 2. Documents, Downloads, Desktop.",
	"pt": "Encontre o PDF. Resuma meus e-mails, a caixa de entrada, a página. Responda ao e-mail. Abra o resultado 2. Deixe mais escuro, use o tema. Documentos, Downloads, Área de trabalho.",
}

// requestPrompt is the recognizer prompt for a whisper language (en,
// pt, auto); BASALT_VOICE_PROMPT replaces the English one.
func (s *service) requestPrompt(wl string) string {
	if wl == "en" || wl == "" {
		return s.cfg.prompt
	}
	return requestPrompts[wl]
}

// whisperArgs builds whisper-cli's arguments for one utterance: the model,
// the file, the language (-l auto, en, pt), the threads, greedy decoding,
// the VAD, the prompt and the encoder window.
func whisperArgs(model, file, lang string, threads int, vad, prompt string, audioCtx int) []string {
	args := []string{"-m", model, "-f", file, "-l", lang, "-t", strconv.Itoa(threads), "-nt", "-np", "-bs", "1", "-bo", "1", "-ng"}
	if vad != "" {
		args = append(args, "--vad", "-vm", vad)
	}
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		args = append(args, "--prompt", prompt)
	}
	if audioCtx > 0 {
		args = append(args, "-ac", strconv.Itoa(audioCtx))
	}
	return args
}

// extraPrompt keeps the shell's extra prompt words short and plain: names
// (letters, spaces, apostrophes, hyphens, commas, periods), at most 300
// bytes. They only bias the recognition toward those spellings.
func extraPrompt(p string) string {
	var b strings.Builder
	for _, r := range p {
		if b.Len() >= 300 {
			break
		}
		if unicode.IsLetter(r) || r == ' ' || r == '\'' || r == '-' || r == ',' || r == '.' || r == ':' {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *service) transcribeWith(ctx context.Context, pcm []byte, extra string, dictation bool, model, lang string) (*voice.Transcript, error) {
	start := time.Now()
	f, err := os.CreateTemp(s.cfg.runDir, "utt-*.wav")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer os.Remove(name)
	_ = f.Chmod(0o600)
	if _, err := f.Write(wav(pcm, s.cfg.rate)); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// A short prompt with the words of the desktop's requests, in the
	// speech language; none for dictation (free text).
	base := s.requestPrompt(lang)
	if dictation {
		base = ""
	}
	// The encoder normally works on a 30 s window whatever the length of
	// the utterance; a window fitted to the recording (plus a margin)
	// halved the CPU time in the lab at the same error rate.
	args := whisperArgs(model, name, lang, s.cfg.threads, s.cfg.vadModel, base+" "+extra, audioCtx(len(pcm)/2/s.cfg.rate))
	cmd := exec.CommandContext(ctx, s.cfg.sttBin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("speech to text: %v: %s", err, tailStr(errb.String(), 300))
	}
	text := strings.TrimSpace(reNoise.ReplaceAllString(out.String(), " "))
	text = strings.Join(strings.Fields(text), " ")
	return &voice.Transcript{Text: text, Speech: text != "", STTMS: time.Since(start).Milliseconds(), Model: filepath.Base(model), Lang: lang}, nil
}

// audioCtx is the encoder window for an utterance of secs seconds (1500
// frames are 30 s), at least half the window, 0 (the default) past 14 s.
func audioCtx(secs int) int {
	if os.Getenv("BASALT_VOICE_FULL_WINDOW") == "1" || secs > 14 {
		return 0
	}
	return 768
}

func tailStr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// ------------------------------------------------------------ speech

// tts is a warm Piper process (--json-input): one JSON line per sentence
// in, the path of the WAV file it wrote out.
type tts struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func (s *service) ttsProc(model string) (*tts, error) {
	if s.tts != nil && s.tts.cmd.ProcessState == nil && s.ttsModel == model {
		return s.tts, nil
	}
	if s.tts != nil && s.tts.cmd.Process != nil {
		// Another voice: a new warm process for it.
		_ = s.tts.cmd.Process.Kill()
		s.tts = nil
	}
	args := append([]string{"--model", model, "--json-input", "--output_dir", s.cfg.runDir, "--quiet"}, s.cfg.ttsArgs...)
	cmd := exec.Command(s.cfg.ttsBin, args...)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Dir(s.cfg.ttsBin))
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("text to speech: %w", err)
	}
	s.tts, s.ttsModel = &tts{cmd: cmd, in: in, out: bufio.NewReader(out)}, model
	go func(t *tts) { _ = t.cmd.Wait() }(s.tts)
	return s.tts, nil
}

var reSentence = regexp.MustCompile(`[^.!?]+[.!?]*`)

func sentences(text string) []string {
	var out []string
	for _, m := range reSentence.FindAllString(text, -1) {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func (s *service) synth(t *tts, sentence string, n int) (string, error) {
	path := filepath.Join(s.cfg.runDir, fmt.Sprintf("tts-%d-%d.wav", os.Getpid(), n))
	b, _ := json.Marshal(map[string]string{"text": sentence, "output_file": path})
	if _, err := t.in.Write(append(b, '\n')); err != nil {
		return "", err
	}
	line, err := t.out.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// speak synthesizes sentence by sentence and plays them in order; it
// returns when the first sentence starts playing.
func (s *service) speak(ctx context.Context, text, voiceName string) (*voice.Spoken, error) {
	model, err := s.cfg.sys.Find(voiceName, "tts")
	if err != nil {
		return nil, err
	}
	s.hush()
	start := time.Now()
	parts := sentences(text)
	if len(parts) == 0 {
		return &voice.Spoken{}, nil
	}
	s.mu.Lock()
	s.speakGen++
	gen := s.speakGen
	t, err := s.ttsProc(model)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	files := make(chan string, len(parts))
	first := make(chan error, 1)
	var synthFirst time.Duration
	go func() {
		defer close(files)
		for i, p := range parts {
			ts := time.Now()
			f, err := s.synth(t, p, i)
			if i == 0 {
				synthFirst = time.Since(ts)
			}
			if err != nil {
				if i == 0 {
					first <- err
				}
				return
			}
			files <- f
		}
	}()
	started := make(chan struct{}, 1)
	go func() {
		for f := range files {
			s.mu.Lock()
			if gen != s.speakGen {
				s.mu.Unlock()
				os.Remove(f)
				continue
			}
			p := exec.Command(s.cfg.pwPlay, "--media-role", "Assistant", "-P", `{ application.name = "Basalt voice" }`, f)
			p.Stderr = io.Discard
			if err := p.Start(); err != nil {
				s.mu.Unlock()
				os.Remove(f)
				continue
			}
			s.player, s.state = p, "speaking"
			s.mu.Unlock()
			select {
			case started <- struct{}{}:
			default:
			}
			_ = p.Wait()
			os.Remove(f)
		}
		s.mu.Lock()
		if gen == s.speakGen {
			s.player = nil
			if s.state == "speaking" {
				s.state = "idle"
			}
		}
		s.mu.Unlock()
	}()
	select {
	case <-started:
	case err := <-first:
		return nil, err
	case <-time.After(30 * time.Second):
		return nil, errors.New("text to speech timed out")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &voice.Spoken{FirstAudioMS: time.Since(start).Milliseconds(), SynthMS: synthFirst.Milliseconds(), Sentences: len(parts),
		Voice: filepath.Base(model)}, nil
}

func (s *service) hush() {
	s.mu.Lock()
	s.speakGen++
	if s.player != nil && s.player.Process != nil {
		_ = s.player.Process.Kill()
	}
	s.player = nil
	if s.state == "speaking" {
		s.state = "idle"
	}
	s.mu.Unlock()
}

func (s *service) shutdown() {
	s.cancel()
	s.hush()
	if s.tts != nil && s.tts.cmd.Process != nil {
		_ = s.tts.cmd.Process.Kill()
	}
}
