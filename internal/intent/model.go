package intent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Model is the optional language-model translator: an OpenAI-compatible
// chat completions endpoint (normally the local basalt-llm service, a
// llama.cpp server on a Unix socket). Its answer is constrained by a JSON
// schema of the closed action set and validated again by the shell.
type Model struct {
	Endpoint string // http(s)://host:port or unix:/path/to.sock
	Model    string
	// AllowRemote permits a non-local endpoint (off by default).
	AllowRemote bool
	Timeout     time.Duration
}

// FromAssistantConfig reads the [translator] section of the system
// assistant's configuration (/etc/basalt/assistant.conf), so the shell
// uses the same local model as `basalt ask`. Environment variables
// BASALT_SHELL_TRANSLATOR (endpoint) and BASALT_SHELL_TRANSLATOR_MODEL
// override it. Returns nil when no translator is configured.
func FromAssistantConfig(path string) *Model {
	m := &Model{Timeout: 30 * time.Second}
	enabled := false
	if f, err := os.Open(path); err == nil {
		sec := ""
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, ";") {
				continue
			}
			if strings.HasPrefix(l, "[") {
				sec = strings.Trim(l, "[]")
				continue
			}
			k, v, ok := strings.Cut(l, "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch {
			case sec == "translator" && k == "enabled":
				enabled = v == "yes" || v == "true" || v == "1"
			case sec == "translator" && k == "endpoint":
				m.Endpoint = v
			case sec == "translator" && k == "model":
				m.Model = v
			case k == "allow_remote":
				m.AllowRemote = v == "yes" || v == "true"
			}
		}
		f.Close()
		if enabled && m.Endpoint == "" {
			m.Endpoint = "unix:/run/basalt-llm/llm.sock"
		}
	}
	if e := os.Getenv("BASALT_SHELL_TRANSLATOR"); e != "" {
		m.Endpoint, enabled = e, true
	}
	if e := os.Getenv("BASALT_SHELL_TRANSLATOR_MODEL"); e != "" {
		m.Model = e
	}
	if !enabled || m.Endpoint == "" {
		return nil
	}
	return m
}

// Local reports whether the endpoint is on this machine.
func (m *Model) Local() bool {
	if strings.HasPrefix(m.Endpoint, "unix:") {
		return true
	}
	for _, h := range []string{"://127.", "://localhost", "://[::1]"} {
		if strings.Contains(m.Endpoint, h) {
			return true
		}
	}
	return false
}

func (m *Model) client() (*http.Client, string) {
	if strings.HasPrefix(m.Endpoint, "unix:") {
		sock := strings.TrimPrefix(m.Endpoint, "unix:")
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}}
		return &http.Client{Transport: tr, Timeout: m.Timeout}, "http://local"
	}
	return &http.Client{Timeout: m.Timeout}, strings.TrimRight(m.Endpoint, "/")
}

// ActionInfo is the part of an action definition the prompt needs.
type ActionInfo struct {
	Name        string
	Description string
	Params      string // compact "name:type" list
}

// Translate asks the model. ctx tokens and actions go into the prompt.
func (m *Model) Translate(ctx context.Context, text string, c Context, actions []ActionInfo) (Result, error) {
	if !m.Local() && !m.AllowRemote {
		return Result{}, errors.New("translator endpoint is not local and allow_remote is off")
	}
	names := make([]string, 0, len(actions))
	var b strings.Builder
	b.WriteString("You translate a request to the Basalt OS desktop shell into typed actions. You never run anything; the person confirms every change.\n\nActions:\n")
	for _, a := range actions {
		names = append(names, a.Name)
		fmt.Fprintf(&b, "- %s(%s): %s\n", a.Name, a.Params, a.Description)
	}
	b.WriteString("\nCurrent theme tokens (key = value):\n")
	keys := make([]string, 0, len(c.Tokens))
	for k := range c.Tokens {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %v\n", k, c.Tokens[k])
	}
	b.WriteString("\nThemes: ")
	for _, t := range c.Themes {
		fmt.Fprintf(&b, "%s (%s) ", t.ID, t.Name)
	}
	b.WriteString("\n\nRules: \"darker\" in dark mode lowers the lightness of color.bg, color.surface, color.surfaceAlt and color.border; in light mode it switches to dark mode. \"rounder corners\" raises radius.sm, radius.md, radius.lg and radius.window by about half. Questions about the operating system itself (services, SELinux, disk, snapshots, updates) are not desktop actions: put them in \"system\" as basalt CLI words (for example [\"why\",\"nginx\"]) and leave calls empty. If nothing fits, return no calls and explain why.\n")
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"calls": map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"enum": names},
					"args":   map[string]any{"type": "object"},
				},
				"required": []string{"action", "args"},
			}},
			"system":  map[string]any{"type": "array", "maxItems": 4, "items": map[string]any{"type": "string", "pattern": "^[a-z0-9@._:-]{1,64}$"}},
			"explain": map[string]any{"type": "string", "maxLength": 300},
		},
		"required": []string{"calls", "system", "explain"},
	}
	body := map[string]any{
		"model": m.Model,
		"messages": []map[string]string{
			{"role": "system", "content": b.String()},
			{"role": "user", "content": text},
		},
		"temperature": 0,
		"max_tokens":  400,
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "desktop_actions", "strict": true, "schema": schema,
		}},
	}
	raw, _ := json.Marshal(body)
	cl, base := m.client()
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cl.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return Result{}, fmt.Errorf("translator: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rb, &cr); err != nil || len(cr.Choices) == 0 {
		return Result{}, errors.New("translator: bad response")
	}
	var out struct {
		Calls   []Call   `json:"calls"`
		System  []string `json:"system"`
		Explain string   `json:"explain"`
	}
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &out); err != nil {
		return Result{}, fmt.Errorf("translator: answer is not valid JSON: %v", err)
	}
	r := Result{Calls: out.Calls, System: out.System, Backend: "model"}
	if out.Explain != "" {
		r.Explain = []string{out.Explain}
	}
	return r, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
