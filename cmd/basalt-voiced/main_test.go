package main

import (
	"strings"
	"testing"
)

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
