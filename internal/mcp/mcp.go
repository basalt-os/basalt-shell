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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openbasalt/basalt-shell/internal/shell"
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
}

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
	for i := range ts {
		ts[i].Annotations = map[string]any{"readOnlyHint": true, "openWorldHint": false}
	}
	for _, a := range shell.Actions {
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
