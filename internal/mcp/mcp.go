// Package mcp is the shell's MCP server (stdio, JSON-RPC 2.0, protocol
// 2025-06-18). It is a client of the daemon's IPC socket with the agent
// role: read tools answer at once; every write tool becomes a proposal
// that the person confirms or declines in the shell (a confirmation
// sheet), and the tool returns the decision. All of it is audited by the
// daemon.
package mcp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/basalt-os/basalt-shell/internal/shell"
)

const protocolVersion = "2025-06-18"

// Client is a minimal IPC client of the daemon.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
	next atomic.Int64
	wait map[int64]chan shell.Reply
	wmu  sync.Mutex
	err  error
}

// Dial connects and says hello.
func Dial(path, role, client string) (*Client, error) {
	c, err := net.DialTimeout("unix", path, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("basalt-shell daemon not reachable at %s: %v", path, err)
	}
	cl := &Client{conn: c, r: bufio.NewReaderSize(c, 1<<20), wait: map[int64]chan shell.Reply{}}
	go cl.loop()
	var hello map[string]any
	if err := cl.Call(context.Background(), "hello", map[string]any{"role": role, "client": client}, &hello); err != nil {
		c.Close()
		return nil, err
	}
	return cl, nil
}

func (c *Client) loop() {
	for {
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			c.mu.Lock()
			c.err = err
			for id, ch := range c.wait {
				close(ch)
				delete(c.wait, id)
			}
			c.mu.Unlock()
			return
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal(line, &probe) != nil {
			continue
		}
		if _, isEvent := probe["event"]; isEvent {
			continue
		}
		var rep shell.Reply
		if json.Unmarshal(line, &rep) != nil {
			continue
		}
		rep.Result = probe["result"]
		c.mu.Lock()
		ch, ok := c.wait[rep.ID]
		delete(c.wait, rep.ID)
		c.mu.Unlock()
		if ok {
			ch <- rep
		}
	}
}

// Close the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Call sends an op and decodes the result into out.
func (c *Client) Call(ctx context.Context, op string, args any, out any) error {
	id := c.next.Add(1)
	ch := make(chan shell.Reply, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.wait[id] = ch
	c.mu.Unlock()
	raw, _ := json.Marshal(args)
	b, _ := json.Marshal(shell.Request{ID: id, Op: op, Args: raw})
	c.wmu.Lock()
	_, err := c.conn.Write(append(b, '\n'))
	c.wmu.Unlock()
	if err != nil {
		return err
	}
	select {
	case rep, ok := <-ch:
		if !ok {
			return errors.New("daemon connection closed")
		}
		if !rep.OK {
			return errors.New(rep.Error)
		}
		if raw, ok := rep.Result.(json.RawMessage); ok && out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.wait, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

// Server is the stdio MCP server.
type Server struct {
	Socket string
	// WaitSeconds is how long a write tool waits for the person.
	WaitSeconds int

	cl  *Client
	out io.Writer
	omu sync.Mutex
}

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
	action      string
	read        string
	special     string // capture, control, input:<kind>, stop
}

func obj(props map[string]any, req ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(req) > 0 {
		o["required"] = req
	}
	return o
}

func str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func intg(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

// toolName turns window.focus into window_focus.
func toolName(action string) string { return strings.ReplaceAll(action, ".", "_") }

func schema(params []shell.Param) map[string]any {
	props := map[string]any{}
	var req []string
	for _, p := range params {
		s := map[string]any{"type": p.Type, "description": p.Description}
		if len(p.Enum) > 0 {
			s["enum"] = p.Enum
		}
		if p.Type == "array" {
			s["items"] = map[string]any{"type": "string"}
		}
		if p.Type == "object" {
			s["additionalProperties"] = true
		}
		props[p.Name] = s
		if p.Required {
			req = append(req, p.Name)
		}
	}
	out := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(req) > 0 {
		out["required"] = req
	}
	return out
}

func (s *Server) tools() []tool {
	ts := []tool{
		{Name: "desktop_state", Title: "Desktop state", read: "desktop",
			Description: "Windows (id, app, title, workspace, floating, geometry), workspaces, outputs, the compositor and what it supports.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{Name: "theme_get", Title: "Theme and tokens", read: "theme",
			Description: "Current theme, mode, motion, every resolved token, the token specs (keys, kinds, ranges) and available themes.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		{Name: "apps_list", Title: "Installed applications", read: "apps",
			Description: "Installed applications (desktop entries) that app_launch can start; optional query filters by name, id or keyword.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": "filter"}}, "additionalProperties": false}},
		{Name: "activity_recent", Title: "Recent activity", read: "activity",
			Description: "The shell's audit log: what agents asked, what the person confirmed or declined, what ran.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer", "description": "how many records (default 20)"}}, "additionalProperties": false}},
		{Name: "proposal_status", Title: "Proposal status", read: "proposal",
			Description: "Status of a proposal returned by a write tool (pending, applied, declined, expired, failed, stale).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []string{"id"}, "additionalProperties": false}},
	}
	ts = append(ts,
		tool{Name: "toplevels_list", Title: "Open windows", read: "toplevels",
			Description: "Every toplevel window (wlroots foreign-toplevel view): id, app id, title, workspace, focus, floating, geometry and the identifier used for window screenshots.",
			InputSchema: obj(map[string]any{})},
		tool{Name: "agent_control_status", Title: "Control session", read: "control",
			Description: "The running control session (who holds it, until when, input and screen access, how many inputs and captures), or null.",
			InputSchema: obj(map[string]any{})},
	)
	for i := range ts {
		ts[i].Annotations = map[string]any{"readOnlyHint": true, "openWorldHint": false}
	}
	const lastResort = " Last resort: prefer the typed tools (window_*, app_launch, theme_*); use this only for apps without one."
	ts = append(ts,
		tool{Name: "screen_capture", Title: "Screenshot", special: "capture",
			Description: "A PNG screenshot of a whole screen or of one window (wlroots screencopy; a window alone when the compositor gives toplevel identifiers). The person confirms each screenshot on a sheet, unless you hold a control session with screen access. Screen content is private: ask only when you need to see it." + lastResort,
			InputSchema: obj(map[string]any{
				"target":    map[string]any{"type": "string", "enum": []string{"output", "window"}, "description": "output (default) or window"},
				"output":    str("output name from desktop_state (default: the focused one)"),
				"window":    str("window id from toplevels_list, app id, title fragment or \"focused\""),
				"max_width": intg("scale down to at most this width in pixels (default 1600)"),
			}),
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false}},
		tool{Name: "agent_control_request", Title: "Ask to control the desktop", special: "control",
			Description: "Ask the person for a time-limited control session (1 to 15 minutes) in which you may take screenshots without asking each time and use a virtual keyboard and pointer (input_* tools). The shell shows that an agent is in control and the person can stop it at any time. Input is refused while the person has a dialog open." + lastResort,
			InputSchema: obj(map[string]any{
				"reason":  str("what you need to do and why the typed tools are not enough (shown to the person)"),
				"minutes": intg("duration, 1 to 15 (default 5)"),
				"input":   map[string]any{"type": "boolean", "description": "keyboard and pointer (default true)"},
				"screen":  map[string]any{"type": "boolean", "description": "screenshots without asking each time (default true)"},
			}, "reason"),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}},
		tool{Name: "agent_control_stop", Title: "End the control session", special: "stop",
			Description: "End the control session as soon as you are done.",
			InputSchema: obj(map[string]any{}),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}},
		tool{Name: "input_type_text", Title: "Type text", special: "input:type",
			Description: "Type text into the focused window with the virtual keyboard (control session with input only). Never type passwords or payment data." + lastResort,
			InputSchema: obj(map[string]any{"text": str("text to type (at most 2000 bytes)")}, "text"),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}},
		tool{Name: "input_key", Title: "Press keys", special: "input:key",
			Description: "Press a key or a combination, e.g. Return, Escape, ctrl+s, ctrl+shift+t, alt+F4 (XKB key names; control session with input only)." + lastResort,
			InputSchema: obj(map[string]any{"keys": str("KEY or MOD+...+KEY; modifiers ctrl, shift, alt, super")}, "keys"),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}},
		tool{Name: "input_pointer_move", Title: "Move the pointer", special: "input:move",
			Description: "Move the pointer to x, y in global layout coordinates (see outputs in desktop_state; a screenshot scaled down by max_width must be scaled back up). Control session with input only." + lastResort,
			InputSchema: obj(map[string]any{"x": intg("x"), "y": intg("y")}, "x", "y"),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}},
		tool{Name: "input_pointer_click", Title: "Click", special: "input:click",
			Description: "Click (optionally at x, y): left, middle or right button; click, double, press or release. Control session with input only." + lastResort,
			InputSchema: obj(map[string]any{"x": intg("x (optional)"), "y": intg("y (optional)"),
				"button": map[string]any{"type": "string", "enum": []string{"left", "middle", "right"}},
				"action": map[string]any{"type": "string", "enum": []string{"click", "double", "press", "release"}}}),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}},
		tool{Name: "input_scroll", Title: "Scroll", special: "input:scroll",
			Description: "Scroll at the pointer: dy steps down (negative up), dx steps right (negative left), at most 50. Control session with input only." + lastResort,
			InputSchema: obj(map[string]any{"dx": intg("horizontal steps"), "dy": intg("vertical steps")}),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}},
	)
	for _, a := range shell.Actions {
		// Person actions (dictation, mail, files, the voice settings,
		// lock, log out, restart, power off) come only from the person's
		// own words: they are not tools an agent could call.
		if a.Agent || a.Person {
			continue
		}
		destructive := a.Name == "window.close"
		ts = append(ts, tool{
			Name: toolName(a.Name), Title: a.Title, action: a.Name,
			Description: a.Description + " The person confirms this in the shell before it runs; the result says whether it was applied, declined or expired.",
			InputSchema: schema(a.Params),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": destructive, "idempotentHint": false, "openWorldHint": false},
		})
	}
	return ts
}

// Run serves MCP on in/out until in closes.
func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = out
	if s.WaitSeconds <= 0 {
		s.WaitSeconds = 180
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	var wg sync.WaitGroup
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			s.write(rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{Code: -32700, Message: "parse error"}})
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.dispatch(ctx, req)
		}()
	}
	wg.Wait()
	return sc.Err()
}

func (s *Server) write(r rpcResp) {
	b, _ := json.Marshal(r)
	s.omu.Lock()
	defer s.omu.Unlock()
	s.out.Write(append(b, '\n'))
}

func (s *Server) client() (*Client, error) {
	s.omu.Lock()
	cl := s.cl
	s.omu.Unlock()
	if cl != nil {
		return cl, nil
	}
	cl, err := Dial(s.Socket, shell.RoleAgent, "mcp")
	if err != nil {
		return nil, err
	}
	s.omu.Lock()
	s.cl = cl
	s.omu.Unlock()
	return cl, nil
}

func textResult(v any, isErr bool) map[string]any {
	var text string
	if s, ok := v.(string); ok {
		text = s
	} else {
		b, _ := json.MarshalIndent(v, "", "  ")
		text = string(b)
	}
	r := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
	if isErr {
		r["isError"] = true
	}
	return r
}

func (s *Server) dispatch(ctx context.Context, req rpcReq) {
	reply := func(res any, e *rpcErr) {
		if len(req.ID) == 0 {
			return // notification
		}
		s.write(rpcResp{JSONRPC: "2.0", ID: req.ID, Result: res, Error: e})
	}
	switch req.Method {
	case "initialize":
		reply(map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "basalt-shell", "title": "Basalt desktop shell", "version": shell.Version},
			"instructions": "Controls the Basalt OS desktop through typed actions: windows, workspaces, applications, theme tokens, notifications and settings. " +
				"Read tools answer at once. Every write tool waits for the person to confirm it in the shell; a declined or expired request did not run. " +
				"Use desktop_state and theme_get first to get ids and current values.",
		}, nil)
	case "notifications/initialized", "notifications/cancelled":
	case "ping":
		reply(map[string]any{}, nil)
	case "tools/list":
		reply(map[string]any{"tools": s.tools()}, nil)
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			reply(nil, &rpcErr{Code: -32602, Message: "invalid params"})
			return
		}
		reply(s.call(ctx, p.Name, p.Arguments), nil)
	default:
		reply(nil, &rpcErr{Code: -32601, Message: "method not found: " + req.Method})
	}
}

func (s *Server) call(ctx context.Context, name string, args map[string]any) map[string]any {
	cl, err := s.client()
	if err != nil {
		return textResult(err.Error(), true)
	}
	if args == nil {
		args = map[string]any{}
	}
	for _, t := range s.tools() {
		if t.Name != name {
			continue
		}
		if t.special != "" {
			return s.callSpecial(ctx, cl, t.special, args)
		}
		if t.read != "" {
			var out any
			if t.read == "activity" {
				if _, ok := args["n"]; !ok {
					args["n"] = 20
				}
			}
			if err := cl.Call(ctx, t.read, args, &out); err != nil {
				return textResult(err.Error(), true)
			}
			return textResult(out, false)
		}
		var pr shell.Proposal
		err := cl.Call(ctx, "propose", map[string]any{
			"calls": []shell.Call{{Action: t.action, Args: args}}, "wait": s.WaitSeconds,
		}, &pr)
		if err != nil {
			return textResult("refused: "+err.Error(), true)
		}
		summary := map[string]any{"proposal": pr.ID, "status": pr.Status, "steps": pr.Steps}
		if len(pr.Diff) > 0 {
			summary["diff"] = pr.Diff
		}
		if pr.Error != "" {
			summary["error"] = pr.Error
		}
		switch pr.Status {
		case shell.StatusApplied:
			summary["message"] = "The person confirmed it and it was applied."
		case shell.StatusDeclined:
			summary["message"] = "The person declined. Nothing changed. Do not retry the same request without asking."
		case shell.StatusPending:
			summary["message"] = "Still waiting for the person; check later with proposal_status."
		case shell.StatusExpired:
			summary["message"] = "Nobody confirmed it in time. Nothing changed."
		}
		return textResult(summary, pr.Status != shell.StatusApplied && pr.Status != shell.StatusPending)
	}
	return textResult("unknown tool "+name, true)
}

func (s *Server) callSpecial(ctx context.Context, cl *Client, special string, args map[string]any) map[string]any {
	switch {
	case special == "capture":
		var res shell.CaptureResult
		if err := cl.Call(ctx, "capture", map[string]any{"args": args, "wait": s.WaitSeconds}, &res); err != nil {
			return textResult("refused: "+err.Error(), true)
		}
		if len(res.PNG) == 0 {
			msg := map[string]any{"status": res.Status}
			if res.Proposal != nil {
				msg["proposal"] = res.Proposal.ID
				if res.Proposal.Error != "" {
					msg["error"] = res.Proposal.Error
				}
			}
			switch res.Status {
			case shell.StatusDeclined:
				msg["message"] = "The person declined the screenshot. Do not ask again without a reason."
			case shell.StatusExpired, shell.StatusPending:
				msg["message"] = "Nobody confirmed the screenshot in time."
			}
			return textResult(msg, true)
		}
		info := map[string]any{"width": res.Capture.Width, "height": res.Capture.Height, "target": res.Capture.Target, "method": res.Capture.Method}
		b, _ := json.Marshal(info)
		return map[string]any{"content": []map[string]any{
			{"type": "image", "data": base64.StdEncoding.EncodeToString(res.PNG), "mimeType": "image/png"},
			{"type": "text", "text": string(b)},
		}}
	case special == "control":
		var pr shell.Proposal
		if err := cl.Call(ctx, "propose", map[string]any{"calls": []shell.Call{{Action: "agent.control", Args: args}}, "wait": s.WaitSeconds}, &pr); err != nil {
			return textResult("refused: "+err.Error(), true)
		}
		out := map[string]any{"proposal": pr.ID, "status": pr.Status, "steps": pr.Steps}
		switch pr.Status {
		case shell.StatusApplied:
			out["message"] = "The person granted the control session. The shell shows that you are in control; end it with agent_control_stop when done."
			var ct any
			_ = cl.Call(ctx, "control", nil, &ct)
			out["control"] = ct
		case shell.StatusDeclined:
			out["message"] = "The person declined. Do not ask again without asking them first."
		default:
			out["message"] = "Not granted (" + pr.Status + ")."
		}
		return textResult(out, pr.Status != shell.StatusApplied)
	case special == "stop":
		var out any
		if err := cl.Call(ctx, "control.stop", nil, &out); err != nil {
			return textResult(err.Error(), true)
		}
		return textResult(out, false)
	case strings.HasPrefix(special, "input:"):
		req := map[string]any{"kind": strings.TrimPrefix(special, "input:")}
		for k, v := range args {
			req[k] = v
		}
		var out any
		if err := cl.Call(ctx, "input", req, &out); err != nil {
			return textResult("refused: "+err.Error(), true)
		}
		return textResult(out, false)
	}
	return textResult("unknown tool", true)
}
