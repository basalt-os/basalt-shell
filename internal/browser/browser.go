// Package browser reads a web page for the browse skill with headless
// Chromium driven over the DevTools protocol on a private pipe
// (--remote-debugging-pipe: no TCP port is opened). The browser runs in
// its own, throw-away profile (never the person's), and every request it
// makes is paused and checked (Fetch domain): only GET requests to the
// hosts of the grant go out; everything else (another host, a form
// submission, a redirect elsewhere) is refused and reported. The
// kernel's per-session filter (basalt-resolver) is the boundary below
// this one. The page's text is extracted in an isolated JavaScript world
// (the page's own scripts cannot change the functions used), separating
// what a person would see from text hidden by styles.
package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/basalt-os/basalt-shell/internal/guard"
)

// Options for one page read.
type Options struct {
	URL      string
	Allow    []string // hosts the browser may contact (exact or *.suffix)
	Chromium string   // program (default: chromium-browser, chromium)
	Profile  string   // throw-away profile directory (created, then removed)
	Timeout  time.Duration
	Proxy    string // optional proxy for the browser (http://127.0.0.1:PORT)
}

// Request is one network request the page tried to make.
type Request struct {
	URL     string `json:"url"`
	Host    string `json:"host"`
	Method  string `json:"method"`
	Type    string `json:"type"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// Page is what the skill gets back.
type Page struct {
	URL      string       `json:"url"`
	Final    string       `json:"final_url"`
	Title    string       `json:"title"`
	Visible  string       `json:"visible"`
	Hidden   string       `json:"hidden,omitempty"`
	Links    []guard.Link `json:"links,omitempty"`
	Forms    int          `json:"forms"`
	Requests []Request    `json:"requests"`
	Blocked  int          `json:"blocked"`
	Report   guard.Report `json:"report"`
	Error    string       `json:"error,omitempty"`
	LoadMS   int64        `json:"load_ms"`
}

type msg struct {
	ID        int64                     `json:"id,omitempty"`
	Method    string                    `json:"method,omitempty"`
	Params    json.RawMessage           `json:"params,omitempty"`
	Result    json.RawMessage           `json:"result,omitempty"`
	Error     *struct{ Message string } `json:"error,omitempty"`
	SessionID string                    `json:"sessionId,omitempty"`
}

type conn struct {
	w      *os.File
	mu     sync.Mutex
	next   int64
	wait   map[int64]chan msg
	events chan msg
}

func (c *conn) send(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan msg, 1)
	c.wait[id] = ch
	c.mu.Unlock()
	m := map[string]any{"id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	if session != "" {
		m["sessionId"] = session
	}
	b, _ := json.Marshal(m)
	c.mu.Lock()
	_, err := c.w.Write(append(b, 0))
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *conn) read(r *os.File) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		b, err := br.ReadBytes(0)
		if err != nil {
			close(c.events)
			return
		}
		var m msg
		if json.Unmarshal(b[:len(b)-1], &m) != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.wait[m.ID]
			delete(c.wait, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		select {
		case c.events <- m:
		default:
			// A page flooding events: drop (requests stay paused and
			// time out, which fails closed).
		}
	}
}

// Program finds the Chromium executable.
func Program() string {
	for _, p := range []string{"chromium-browser", "chromium", "headless_shell"} {
		if x, err := exec.LookPath(p); err == nil {
			return x
		}
	}
	return ""
}

// Read loads one page and extracts its text.
func Read(ctx context.Context, o Options) (*Page, error) {
	if o.Timeout == 0 {
		o.Timeout = 25 * time.Second
	}
	if o.Chromium == "" {
		o.Chromium = Program()
	}
	if o.Chromium == "" {
		return nil, errors.New("no Chromium installed")
	}
	u, err := url.Parse(o.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("not a web address: %q", o.URL)
	}
	if !guard.HostAllowed(o.URL, o.Allow) {
		return nil, fmt.Errorf("%s is not in the sites you allowed", u.Hostname())
	}
	if o.Profile == "" {
		d, err := os.MkdirTemp("", "basalt-skill-browser-")
		if err != nil {
			return nil, err
		}
		o.Profile = d
	} else if err := os.MkdirAll(o.Profile, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(o.Profile)
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	// fd 3: the browser reads commands; fd 4: it writes replies.
	cmdR, cmdW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	args := []string{
		"--headless=new", "--remote-debugging-pipe", "--user-data-dir=" + o.Profile,
		"--no-first-run", "--no-default-browser-check", "--disable-sync", "--disable-extensions",
		"--disable-background-networking", "--disable-component-update", "--disable-default-apps",
		"--disable-domain-reliability", "--disable-client-side-phishing-detection", "--no-pings",
		"--dns-prefetch-disable", "--disable-breakpad", "--disable-crash-reporter", "--disable-gpu",
		"--disable-features=Translate,OptimizationHints,MediaRouter,DialMediaRouteProvider,AutofillServerCommunication,InterestFeedContentSuggestions,CertificateTransparencyComponentUpdater",
		"--metrics-recording-only", "--password-store=basic", "--use-mock-keychain",
		"--block-new-web-contents", "--mute-audio", "--window-size=1280,1800", "--lang=en-US",
	}
	if o.Proxy != "" {
		args = append(args, "--proxy-server="+o.Proxy, "--proxy-bypass-list=<-loopback>")
	}
	args = append(args, "about:blank")
	cmd := exec.Command(o.Chromium, args...)
	cmd.ExtraFiles = []*os.File{cmdR, outW}
	cmd.Env = append(os.Environ(), "HOME="+o.Profile, "XDG_CONFIG_HOME="+filepath.Join(o.Profile, "config"), "XDG_CACHE_HOME="+filepath.Join(o.Profile, "cache"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	var stderr strings.Builder
	cmd.Stderr = &limitWriter{w: &stderr, n: 8192}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	cmdR.Close()
	outW.Close()
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()
	c := &conn{w: cmdW, wait: map[int64]chan msg{}, events: make(chan msg, 4096)}
	go c.read(outR)

	start := time.Now()
	page := &Page{URL: o.URL}
	res, err := c.send(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("browser did not start: %v (%s)", err, strings.TrimSpace(stderr.String()))
	}
	var tgt struct{ TargetID string }
	_ = json.Unmarshal(res, &tgt)
	res, err = c.send(ctx, "", "Target.attachToTarget", map[string]any{"targetId": tgt.TargetID, "flatten": true})
	if err != nil {
		return nil, err
	}
	var att struct{ SessionID string }
	_ = json.Unmarshal(res, &att)
	s := att.SessionID
	for _, m := range []string{"Page.enable", "Runtime.enable"} {
		if _, err := c.send(ctx, s, m, nil); err != nil {
			return nil, err
		}
	}
	if _, err := c.send(ctx, s, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}); err != nil {
		return nil, err
	}
	// No downloads, no new windows.
	_, _ = c.send(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny"})

	loaded := make(chan struct{}, 1)
	var reqMu sync.Mutex
	go func() {
		for ev := range c.events {
			switch ev.Method {
			case "Fetch.requestPaused":
				var p struct {
					RequestID    string `json:"requestId"`
					ResourceType string `json:"resourceType"`
					Request      struct {
						URL    string `json:"url"`
						Method string `json:"method"`
					} `json:"request"`
				}
				_ = json.Unmarshal(ev.Params, &p)
				r := Request{URL: p.Request.URL, Method: p.Request.Method, Type: p.ResourceType, Host: hostOf(p.Request.URL)}
				switch {
				case strings.HasPrefix(p.Request.URL, "data:"):
					r.Allowed = true
				case !guard.HostAllowed(p.Request.URL, o.Allow):
					r.Reason = "host not allowed"
				case p.Request.Method != "GET" && p.Request.Method != "HEAD":
					r.Reason = "read only: " + p.Request.Method + " refused"
				default:
					r.Allowed = true
				}
				reqMu.Lock()
				if len(page.Requests) < 500 {
					if len(r.URL) > 300 {
						r.URL = r.URL[:300]
					}
					page.Requests = append(page.Requests, r)
				}
				if !r.Allowed {
					page.Blocked++
				}
				reqMu.Unlock()
				if r.Allowed {
					go c.send(ctx, ev.SessionID, "Fetch.continueRequest", map[string]any{"requestId": p.RequestID})
				} else {
					go c.send(ctx, ev.SessionID, "Fetch.failRequest", map[string]any{"requestId": p.RequestID, "errorReason": "BlockedByClient"})
				}
			case "Page.loadEventFired":
				select {
				case loaded <- struct{}{}:
				default:
				}
			}
		}
	}()
	nav, err := c.send(ctx, s, "Page.navigate", map[string]any{"url": o.URL})
	if err != nil {
		return nil, err
	}
	var nr struct {
		FrameID   string `json:"frameId"`
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(nav, &nr)
	if nr.ErrorText != "" {
		page.Error = nr.ErrorText
	}
	select {
	case <-loaded:
	case <-time.After(15 * time.Second):
		page.Error = strings.TrimSpace(page.Error + " the page did not finish loading in 15 s")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Give the page's scripts a moment (what they try to fetch is
	// recorded, and refused when not allowed).
	time.Sleep(1200 * time.Millisecond)
	page.LoadMS = time.Since(start).Milliseconds()

	// Extract in an isolated world, so the page cannot redefine what the
	// extractor uses.
	frame := nr.FrameID
	if frame == "" {
		if ft, err := c.send(ctx, s, "Page.getFrameTree", nil); err == nil {
			var t struct {
				FrameTree struct {
					Frame struct{ ID string } `json:"frame"`
				} `json:"frameTree"`
			}
			_ = json.Unmarshal(ft, &t)
			frame = t.FrameTree.Frame.ID
		}
	}
	iw, err := c.send(ctx, s, "Page.createIsolatedWorld", map[string]any{"frameId": frame, "worldName": "basalt-skill", "grantUniveralAccess": false})
	if err != nil {
		return nil, err
	}
	var w struct {
		ExecutionContextID int `json:"executionContextId"`
	}
	_ = json.Unmarshal(iw, &w)
	ev, err := c.send(ctx, s, "Runtime.evaluate", map[string]any{"expression": extractJS, "contextId": w.ExecutionContextID, "returnByValue": true, "timeout": 5000})
	if err != nil {
		return nil, err
	}
	var er struct {
		Result struct {
			Value struct {
				Title   string       `json:"title"`
				URL     string       `json:"url"`
				Visible string       `json:"visible"`
				Hidden  []string     `json:"hidden"`
				Links   []guard.Link `json:"links"`
				Forms   int          `json:"forms"`
				Refresh string       `json:"refresh"`
			} `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct{ Text string } `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(ev, &er); err != nil {
		return nil, err
	}
	if er.ExceptionDetails != nil {
		return nil, errors.New("extracting the page failed: " + er.ExceptionDetails.Text)
	}
	v := er.Result.Value
	page.Title, page.Final, page.Forms = strings.TrimSpace(v.Title), v.URL, v.Forms
	page.Visible = strings.TrimSpace(v.Visible)
	if len(page.Visible) > 60000 {
		page.Visible = page.Visible[:60000]
	}
	page.Hidden = strings.TrimSpace(strings.Join(v.Hidden, "\n"))
	if len(page.Hidden) > 20000 {
		page.Hidden = page.Hidden[:20000]
	}
	page.Links = v.Links
	// Findings: hidden text, what the visible text says, the links, and
	// every refused request (a page that tries to reach another host while
	// being read is the exfiltration attempt).
	var rep guard.Report
	if page.Hidden != "" {
		sub := guard.Scan(page.Hidden)
		sev := "low"
		if sub.Suspicious() {
			sev = "high"
		}
		rep.Add(guard.Finding{Kind: guard.KindHidden, Severity: sev, Detail: "text a reader would not see", Snippet: clipStr(page.Hidden, 140)})
		rep.Add(sub.Findings...)
	}
	rep.Add(guard.Scan(page.Title + "\n" + page.Visible).Findings...)
	for _, l := range page.Links {
		guard.CheckLinkText(&rep, l.Text, "http://"+l.Host+"/")
	}
	reqMu.Lock()
	hosts := map[string]bool{}
	for _, r := range page.Requests {
		if !r.Allowed && !hosts[r.Host+r.Reason] {
			hosts[r.Host+r.Reason] = true
			sev := "high"
			detail := "the page tried to contact " + r.Host + " (blocked)"
			if strings.HasPrefix(r.Reason, "read only") {
				detail = "the page tried to send data (" + r.Method + " to " + r.Host + ", blocked)"
			}
			rep.Add(guard.Finding{Kind: guard.KindExfil, Severity: sev, Detail: detail, Snippet: clipStr(r.URL, 120)})
		}
	}
	reqMu.Unlock()
	if v.Refresh != "" {
		rep.Add(guard.Finding{Kind: guard.KindLink, Severity: "medium", Detail: "the page redirects by itself", Snippet: clipStr(v.Refresh, 100)})
	}
	if page.Final != "" && !guard.HostAllowed(page.Final, o.Allow) && !strings.HasPrefix(page.Final, "chrome-error:") {
		rep.Add(guard.Finding{Kind: guard.KindLink, Severity: "high", Detail: "the page moved to " + hostOf(page.Final)})
	}
	page.Report = rep
	return page, nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func clipStr(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

type limitWriter struct {
	w interface{ WriteString(string) (int, error) }
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := len(p)
		if k > l.n {
			k = l.n
		}
		l.w.WriteString(string(p[:k]))
		l.n -= k
	}
	return len(p), nil
}

// extractJS runs in an isolated world: it walks the text nodes of the
// document and sorts them into what a person would see and what is
// hidden (styles that hide, tiny or transparent text, text colored like
// its background, off-screen or clipped boxes, aria-hidden, comments).
const extractJS = `(() => {
  const vis = [], hid = [];
  const seenHidden = new Set();
  const cs = (el) => getComputedStyle(el);
  const parseRGB = (c) => { const m = /rgba?\(([^)]+)\)/.exec(c || ""); if (!m) return null; const p = m[1].split(",").map(s => parseFloat(s)); return {r:p[0], g:p[1], b:p[2], a: p.length > 3 ? p[3] : 1}; };
  const bgOf = (el) => { for (let e = el; e && e.nodeType === 1; e = e.parentElement) { const c = parseRGB(cs(e).backgroundColor); if (c && c.a > 0.5) return c; } return {r:255,g:255,b:255,a:1}; };
  const close = (a, b) => Math.abs(a.r-b.r) + Math.abs(a.g-b.g) + Math.abs(a.b-b.b) < 40;
  const W = document.documentElement.scrollWidth || 1280, H = Math.max(document.documentElement.scrollHeight, 1800);
  const why = (el) => {
    for (let e = el; e && e.nodeType === 1; e = e.parentElement) {
      const s = cs(e);
      if (s.display === "none") return "display none";
      if (s.visibility === "hidden" || s.visibility === "collapse") return "visibility hidden";
      if (parseFloat(s.opacity) === 0) return "transparent";
      if (e.getAttribute && e.getAttribute("aria-hidden") === "true") return "aria-hidden";
      if (s.clipPath && s.clipPath.startsWith("inset(50%")) return "clipped";
      if (s.clip && s.clip.startsWith("rect(0")) return "clipped";
    }
    const s = cs(el);
    if (parseFloat(s.fontSize) < 3) return "tiny text";
    const fg = parseRGB(s.color);
    if (fg && fg.a < 0.1) return "transparent text";
    if (fg && close(fg, bgOf(el))) return "same color as background";
    const r = el.getBoundingClientRect();
    if (r.width < 2 || r.height < 2) return "zero size";
    if (r.right < 0 || r.bottom < 0 || r.left > W + 50 || r.top > H + 2000) return "off screen";
    return "";
  };
  const skip = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD", "TITLE", "META", "SVG"]);
  const walker = document.createTreeWalker(document.documentElement, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_COMMENT);
  let n, count = 0;
  while ((n = walker.nextNode()) && count < 20000) {
    count++;
    const t = (n.nodeValue || "").replace(/\s+/g, " ").trim();
    if (!t) continue;
    if (n.nodeType === 8) { hid.push("comment: " + t); continue; }
    const el = n.parentElement;
    if (!el || skip.has(el.tagName)) { if (el && el.tagName === "NOSCRIPT") hid.push(t); continue; }
    const w = why(el);
    if (w) { if (!seenHidden.has(t)) { seenHidden.add(t); hid.push(t); } }
    else vis.push(t);
  }
  // Inputs' placeholders and alt text are visible content too.
  document.querySelectorAll("img[alt]").forEach(i => { if (i.alt.trim() && !why(i)) vis.push(i.alt.trim()); });
  const links = [];
  document.querySelectorAll("a[href]").forEach(a => { if (links.length < 200) { try { links.push({text: (a.innerText || "").trim().slice(0, 80), host: new URL(a.href).hostname}); } catch (e) {} } });
  const meta = document.querySelector("meta[http-equiv=refresh i]");
  return { title: document.title || "", url: location.href, visible: vis.join("\n"), hidden: hid, links: links,
           forms: document.forms.length, refresh: meta ? meta.content : "" };
})()`
