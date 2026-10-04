// Command basalt-shelld is the Basalt desktop shell daemon: the single
// owner of desktop state and of every change (compositor adapter, design
// tokens, typed actions and proposals, audit log, local socket). It is
// started by the session (the compositor's config) and, with the Basalt
// SELinux policy, runs in its own domain (basalt_shell_t).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/basalt-os/basalt-shell/internal/agentio"
	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/audit"
	"github.com/basalt-os/basalt-shell/internal/compositor"
	_ "github.com/basalt-os/basalt-shell/internal/compositor/fake"
	_ "github.com/basalt-os/basalt-shell/internal/compositor/niri"
	_ "github.com/basalt-os/basalt-shell/internal/compositor/sway"
	"github.com/basalt-os/basalt-shell/internal/hw"
	"github.com/basalt-os/basalt-shell/internal/intent"
	"github.com/basalt-os/basalt-shell/internal/ledger"
	"github.com/basalt-os/basalt-shell/internal/paths"
	"github.com/basalt-os/basalt-shell/internal/shell"
	"github.com/basalt-os/basalt-shell/internal/skills"
	"github.com/basalt-os/basalt-shell/internal/theme"
	"github.com/basalt-os/basalt-shell/internal/voice"
)

var version = "0.2.0-dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("basalt-shelld: ")
	shell.Version = version
	if len(os.Args) > 1 && os.Args[1] == "version" {
		os.Stdout.WriteString("basalt-shelld " + version + "\n")
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if len(os.Args) > 1 && os.Args[1] == "translate" {
		translate(ctx, os.Args[2:])
		return
	}
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	store := theme.NewStore(paths.ThemeDirs(), filepath.Join(paths.ConfigDir(), "basalt-shell"))
	if err := store.Load(); err != nil {
		return err
	}
	for _, e := range store.LoadErrors() {
		log.Printf("theme: %s", e)
	}
	lg, err := audit.Open(paths.AuditLog())
	if err != nil {
		return err
	}
	comp := compositor.Detect()
	rep := hw.Probe()
	core := shell.New(comp, store, lg, rep, paths.ConfigDir())
	if os.Getenv("BASALT_SHELL_NO_APPS") == "1" {
		core.ApplyApps = false
	}
	core.Tools = agentio.Find()
	// The daemon's own virtual keyboard and pointer (wlroots protocols),
	// held for the whole session when there are no input devices.
	core.VirtualInput = os.Getenv("BASALT_SHELL_VIRTUAL_INPUT") != "0" && os.Getenv("WAYLAND_DISPLAY") != ""
	core.VirtualInputAlways = os.Getenv("BASALT_HEADLESS") == "1"
	if core.VirtualInput && core.VirtualInputAlways {
		if err := core.OpenVirtualInput(); err != nil {
			log.Printf("virtual input: %v", err)
		}
	}
	if b, ok := assistant.Default(); ok {
		core.Assistant = b
	}
	core.Translator = intent.FromAssistantConfig("/etc/basalt/assistant.conf")
	if core.Translator != nil && core.Assistant != nil {
		core.Translator.Helper = core.Assistant.TranslateHelper()
	}
	// The read-only skills share the command bar's model settings (the
	// [translator] section; a remote endpoint only with allow_remote).
	if os.Getenv("BASALT_SHELL_SKILLS") != "0" {
		home, _ := os.UserHomeDir()
		core.Skills = skills.New(home, core.Translator)
		core.WireSkills()
	}
	if os.Getenv("BASALT_SHELL_VOICE") != "0" {
		core.Voice = &voice.Client{Path: voice.DefaultSocket()}
	}
	// The audit trail the shell cannot rewrite (ADR 0010), when installed.
	if os.Getenv("BASALT_SHELL_LEDGER") != "0" {
		core.Ledger = ledger.New(os.Getenv("BASALT_LEDGER_SOCKET"))
	}
	ui := shell.DetectUICheck(os.Getenv("BASALT_SHELL_UI_CHECK"))
	if os.Getenv("BASALT_SHELL_INSECURE_UI") == "1" {
		ui = shell.DetectUICheck(shell.UICheckInsecure)
	}
	log.Printf("compositor %s %s; hardware weak=%v %v; translator=%v; assistant=%v; ui check=%s %s; agent io=%v",
		comp.Name(), comp.Version(ctx), rep.Weak, rep.Reasons, core.Translator != nil, core.Assistant != nil, ui.Mode, ui.Reason, core.AgentIO())
	_, _ = lg.Append("start", "daemon", "basalt-shell "+version+" on "+comp.Name(),
		map[string]any{"hardware": rep, "ui_check": ui, "agent_io": core.AgentIO()})
	core.Refresh(ctx)
	core.ApplyTheme(ctx)
	go core.Watch(ctx)
	go core.WatchVoice(ctx)
	if core.Voice != nil && os.Getenv("BASALT_SHELL_DICTATION") != "0" && os.Getenv("WAYLAND_DISPLAY") != "" {
		// Dictation: the shell is the session's input method.
		go core.WatchInputMethod(ctx)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go exitWithCompositor(ctx, cancel, comp)
	srv := &shell.Server{Core: core, Path: shell.DefaultSocket(), UI: ui}
	// The daemon of a session that just ended may still hold the socket
	// for a few seconds (it exits when its compositor is gone).
	for i := 0; i < 20; i++ {
		c, err := net.Dial("unix", srv.Path)
		if err != nil {
			break
		}
		c.Close()
		if i == 0 {
			log.Printf("waiting for the previous daemon on %s to exit", srv.Path)
		}
		time.Sleep(time.Second)
	}
	log.Printf("listening on %s", srv.Path)
	return srv.Listen(ctx)
}

// exitWithCompositor stops the daemon when its compositor is gone (a
// session ended by the greeter does not always signal the processes the
// compositor started), so the next session gets a fresh daemon.
func exitWithCompositor(ctx context.Context, cancel context.CancelFunc, comp compositor.Adapter) {
	sock := os.Getenv("SWAYSOCK")
	if sock == "" {
		sock = os.Getenv("NIRI_SOCKET")
	}
	if sock == "" || comp.Name() == "none" || comp.Name() == "fake" {
		return
	}
	missing := 0
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		// A socket file can outlive a compositor that was killed: try to
		// connect.
		if c, err := net.DialTimeout("unix", sock, time.Second); err != nil {
			missing++
		} else {
			c.Close()
			missing = 0
		}
		if missing >= 3 {
			log.Printf("compositor socket %s is gone: exiting", sock)
			cancel()
			return
		}
	}
}

// translate runs the command bar's understanding on each line of stdin
// (or the arguments) and prints one JSON result per request, without
// proposing anything: for measuring a model (basalt-shelld translate < requests.txt).
func translate(ctx context.Context, args []string) {
	store := theme.NewStore(paths.ThemeDirs(), filepath.Join(paths.ConfigDir(), "basalt-shell"))
	if err := store.Load(); err != nil {
		log.Fatal(err)
	}
	tok, err := store.Resolve(store.Settings(), false)
	if err != nil {
		log.Fatal(err)
	}
	ictx := intent.Context{Tokens: tok, Themes: store.Themes()}
	m := intent.FromAssistantConfig("/etc/basalt/assistant.conf")
	if m != nil {
		if b, ok := assistant.Default(); ok {
			m.Helper = b.TranslateHelper()
		}
	}
	reqs := args
	if len(reqs) == 0 {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			if t := strings.TrimSpace(sc.Text()); t != "" && !strings.HasPrefix(t, "#") {
				reqs = append(reqs, t)
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	for _, r := range reqs {
		out := map[string]any{"request": r}
		if m != nil {
			res, err := m.Translate(ctx, r, ictx)
			if err != nil {
				out["error"] = err.Error()
			}
			var calls []string
			for _, c := range res.Calls {
				calls = append(calls, fmt.Sprintf("%s %v", c.Action, c.Args))
			}
			out["model"], out["calls"], out["ask_system"], out["clarify"], out["none"] = res.Model, calls, res.AskSystem, res.Clarify, res.None
		}
		rr := intent.Rules(r, ictx)
		var rc []string
		for _, c := range rr.Calls {
			rc = append(rc, fmt.Sprintf("%s %v", c.Action, c.Args))
		}
		out["rules"] = map[string]any{"calls": rc, "system": rr.System, "unknown": rr.Unknown}
		_ = enc.Encode(out)
	}
}
