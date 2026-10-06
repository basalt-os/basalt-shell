package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/voice"
)

// TestUnloadEndsTheWarmSynthesizer: "unload" (spoken answers turned off)
// ends the warm text-to-speech process, and nothing starts one but a
// "speak".
func TestUnloadEndsTheWarmSynthesizer(t *testing.T) {
	dir := t.TempDir()
	piper := filepath.Join(dir, "piper")
	// A stand-in for Piper: it waits on its input like the real one.
	if err := os.WriteFile(piper, []byte("#!/bin/sh\nexec cat >/dev/null\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &service{cfg: config{ttsBin: piper, runDir: dir}}
	if s.tts != nil {
		t.Fatal("a synthesizer runs before any speak")
	}
	s.mu.Lock()
	tp, err := s.ttsProc(filepath.Join(dir, "voice.onnx"))
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if rep := s.handle(context.Background(), voice.Request{Op: "unload"}); !rep.OK {
		t.Fatalf("unload: %+v", rep)
	}
	if s.tts != nil || s.ttsModel != "" {
		t.Error("the warm synthesizer is still referenced")
	}
	// The process is gone (killed, then reaped by ttsProc's goroutine).
	deadline := time.Now().Add(3 * time.Second)
	for tp.cmd.Process.Signal(syscall.Signal(0)) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if tp.cmd.Process.Signal(syscall.Signal(0)) == nil {
		t.Error("the synthesizer process still runs after unload")
	}
	// A second unload with nothing running is fine.
	if rep := s.handle(context.Background(), voice.Request{Op: "unload"}); !rep.OK {
		t.Errorf("second unload: %+v", rep)
	}
}

func TestWhisperArgs(t *testing.T) {
	a := strings.Join(whisperArgs("/m/ggml-small-q5_1.bin", "/run/u.wav", "pt", 4, "/m/vad.bin", "  Encontre o PDF. ", 768), " ")
	for _, want := range []string{"-m /m/ggml-small-q5_1.bin", "-f /run/u.wav", "-l pt", "-t 4", "--vad -vm /m/vad.bin", "--prompt Encontre o PDF.", "-ac 768"} {
		if !strings.Contains(a, want) {
			t.Errorf("args %q lack %q", a, want)
		}
	}
	a = strings.Join(whisperArgs("/m/ggml-base.en.bin", "/run/u.wav", "en", 2, "", " ", 0), " ")
	if strings.Contains(a, "--prompt") || strings.Contains(a, "--vad") || strings.Contains(a, "-ac") || !strings.Contains(a, "-l en") {
		t.Errorf("args %q", a)
	}
}

func TestRequestPrompt(t *testing.T) {
	s := &service{cfg: config{prompt: "Find the PDF."}}
	if s.requestPrompt("en") != "Find the PDF." || !strings.Contains(s.requestPrompt("pt"), "Encontre") || s.requestPrompt("auto") != "" {
		t.Errorf("prompts: %q %q %q", s.requestPrompt("en"), s.requestPrompt("pt"), s.requestPrompt("auto"))
	}
}
