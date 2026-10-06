// Package voice is the shell's side of basalt-voiced, the push-to-talk
// voice service: the newline-JSON protocol on its socket and a client for
// the shell daemon. basalt-voiced owns the microphone only between
// "listen" and "stop" (the person holding the key or the panel button),
// turns speech into text locally (whisper.cpp with Silero VAD) and speaks
// short answers locally (Piper). It has no network and keeps no audio.
package voice

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Request to basalt-voiced.
type Request struct {
	ID   int64  `json:"id"`
	Op   string `json:"op"` // listen, stop, cancel, speak, hush, unload, status, models
	Text string `json:"text,omitempty"`
	// Lang (stop) is the person's speech language: "auto" or a language
	// tag ("pt-BR"); empty means the system's default. Model (stop) is
	// the person's speech model and Voice (speak) their voice, by name;
	// empty means the system's default. The service checks both against
	// the administrator's policy on every request.
	Lang  string `json:"lang,omitempty"`
	Model string `json:"model,omitempty"`
	Voice string `json:"voice,omitempty"`
	// Prompt (stop) adds words to the speech-to-text prompt: the names
	// the person is likely to say (granted contacts and mail senders).
	Prompt string `json:"prompt,omitempty"`
	// Dictation (stop): free text, not a request; the requests'
	// vocabulary prompt is left out (it made Whisper hear "Hi Ana" as
	// "High honor" in the lab).
	Dictation bool `json:"dictation,omitempty"`
}

// Transcript is the result of stop.
type Transcript struct {
	Text      string  `json:"text"`
	Speech    bool    `json:"speech"`     // the VAD found speech
	AudioMS   int64   `json:"audio_ms"`   // how long the key was held
	CaptureMS int64   `json:"capture_ms"` // from stop to the audio being complete
	STTMS     int64   `json:"stt_ms"`     // speech to text (VAD included)
	Model     string  `json:"model"`
	Level     float64 `json:"level"`          // RMS of the recording (0..1)
	Lang      string  `json:"lang,omitempty"` // the -l given to the recognizer (auto, en, pt)
}

// Spoken is the result of speak.
type Spoken struct {
	FirstAudioMS int64  `json:"first_audio_ms"` // from the request to the first sound
	SynthMS      int64  `json:"synth_ms"`       // synthesis of the first sentence
	Sentences    int    `json:"sentences"`
	Voice        string `json:"voice"`
}

// Status of the service.
type Status struct {
	State   string `json:"state"` // idle, listening, transcribing, speaking
	STT     string `json:"stt"`
	TTS     string `json:"tts"`
	Voice   string `json:"voice"`
	Mic     bool   `json:"mic"` // a capture stream is open right now
	MaxHold int    `json:"max_hold_s"`
}

// Reply from basalt-voiced.
type Reply struct {
	ID         int64           `json:"id"`
	OK         bool            `json:"ok"`
	Error      string          `json:"error,omitempty"`
	Code       string          `json:"code,omitempty"` // error code (CodeEnglishOnly and the others)
	Models     *Models         `json:"models,omitempty"`
	Transcript *Transcript     `json:"transcript,omitempty"`
	Spoken     *Spoken         `json:"spoken,omitempty"`
	Status     *Status         `json:"status,omitempty"`
	Extra      json.RawMessage `json:"extra,omitempty"`
}

// DefaultSocket is $XDG_RUNTIME_DIR/basalt-voice/voice.sock.
func DefaultSocket() string {
	if p := os.Getenv("BASALT_VOICE_SOCKET"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("basalt-voice-%d", os.Getuid()))
	}
	return filepath.Join(dir, "basalt-voice", "voice.sock")
}

// Client talks to basalt-voiced (one request at a time per connection;
// the client reconnects when needed).
type Client struct {
	Path string
	mu   sync.Mutex
	conn net.Conn
	r    *bufio.Reader
	next int64
}

// Available reports whether the service's socket exists.
func (c *Client) Available() bool {
	st, err := os.Stat(c.Path)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// Do sends one request and waits for its reply.
func (c *Client) Do(ctx context.Context, req Request) (Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if c.conn == nil {
			conn, err := net.DialTimeout("unix", c.Path, 2*time.Second)
			if err != nil {
				return Reply{}, fmt.Errorf("basalt-voiced: %w", err)
			}
			c.conn, c.r = conn, bufio.NewReader(conn)
		}
		c.next++
		req.ID = c.next
		b, _ := json.Marshal(req)
		dl := time.Now().Add(90 * time.Second)
		if d, ok := ctx.Deadline(); ok {
			dl = d
		}
		_ = c.conn.SetDeadline(dl)
		if _, err := c.conn.Write(append(b, '\n')); err != nil {
			c.conn.Close()
			c.conn = nil
			continue
		}
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			c.conn.Close()
			c.conn = nil
			if attempt == 0 && errors.Is(err, net.ErrClosed) {
				continue
			}
			return Reply{}, fmt.Errorf("basalt-voiced: %w", err)
		}
		var rep Reply
		if err := json.Unmarshal(line, &rep); err != nil {
			return Reply{}, err
		}
		if !rep.OK {
			if rep.Code != "" {
				return rep, &CodedError{Code: rep.Code, Msg: rep.Error}
			}
			return rep, errors.New(rep.Error)
		}
		return rep, nil
	}
	return Reply{}, errors.New("basalt-voiced: not reachable")
}
