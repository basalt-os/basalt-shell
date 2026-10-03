// Command basalt-shell is the daemon, MCP server and command-line client
// of the Basalt OS desktop shell.
//
//	basalt-shell daemon            run the shell daemon (started by the session)
//	basalt-shell mcp               MCP server on stdio (for a local model or an MCP client)
//	basalt-shell ctl OP [JSON]     send one IPC request (agent role) and print the reply
//	basalt-shell propose ACTION [JSON-ARGS] [--wait N]
//	                               propose one typed action; the person confirms it in the shell
//	basalt-shell audit verify      check the activity log's hash chain
//	basalt-shell version
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/openbasalt/basalt-shell/internal/assistant"
	"github.com/openbasalt/basalt-shell/internal/audit"
	"github.com/openbasalt/basalt-shell/internal/compositor"
	_ "github.com/openbasalt/basalt-shell/internal/compositor/fake"
	_ "github.com/openbasalt/basalt-shell/internal/compositor/niri"
	_ "github.com/openbasalt/basalt-shell/internal/compositor/sway"
	"github.com/openbasalt/basalt-shell/internal/hw"
	"github.com/openbasalt/basalt-shell/internal/intent"
	"github.com/openbasalt/basalt-shell/internal/mcp"
	"github.com/openbasalt/basalt-shell/internal/shell"
	"github.com/openbasalt/basalt-shell/internal/theme"
)

var version = "0.1.0-dev"

func usage() {
	fmt.Fprint(os.Stderr, `usage: basalt-shell daemon | mcp | ctl OP [JSON] | propose ACTION [JSON] [--wait N] | audit verify | version
`)
	os.Exit(2)
}

func configDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "basalt-shell")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "basalt-shell")
}

func themeDirs() []string {
	if d := os.Getenv("BASALT_SHELL_THEMES"); d != "" {
		return strings.Split(d, ":")
	}
	dirs := []string{"/usr/share/basalt-shell/themes"}
	if exe, err := os.Executable(); err == nil {
		// Running from a checkout or a ~/.local install.
		for _, rel := range []string{"../share/basalt-shell/themes", "../../themes", "../themes"} {
			p := filepath.Clean(filepath.Join(filepath.Dir(exe), rel))
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				dirs = append(dirs, p)
			}
		}
	}
	return dirs
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("basalt-shell: ")
	shell.Version = version
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch os.Args[1] {
	case "daemon":
		if err := daemon(ctx); err != nil {
			log.Fatal(err)
		}
	case "mcp":
		s := &mcp.Server{Socket: shell.DefaultSocket()}
		if v, err := strconv.Atoi(os.Getenv("BASALT_SHELL_MCP_WAIT")); err == nil {
			s.WaitSeconds = v
		}
		if err := s.Run(ctx, os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
	case "ctl":
		if len(os.Args) < 3 {
			usage()
		}
		var args any
		if len(os.Args) > 3 {
			if err := json.Unmarshal([]byte(os.Args[3]), &args); err != nil {
				log.Fatalf("args: %v", err)
			}
		}
		ctl(ctx, os.Args[2], args)
	case "propose":
		if len(os.Args) < 3 {
			usage()
		}
		args := map[string]any{}
		wait := 120
		rest := os.Args[3:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == "--wait" && i+1 < len(rest) {
				wait, _ = strconv.Atoi(rest[i+1])
				i++
				continue
			}
			if err := json.Unmarshal([]byte(rest[i]), &args); err != nil {
				log.Fatalf("args: %v", err)
			}
		}
		ctl(ctx, "propose", map[string]any{"calls": []shell.Call{{Action: os.Args[2], Args: args}}, "wait": wait})
	case "audit":
		n, err := audit.Verify(filepath.Join(stateDir(), "audit.jsonl"))
		if err != nil {
			fmt.Printf("audit chain BROKEN after %d records: %v\n", n, err)
			os.Exit(1)
		}
		fmt.Printf("audit chain ok: %d records\n", n)
	case "version":
		fmt.Println("basalt-shell", version)
	default:
		usage()
	}
}

func ctl(ctx context.Context, op string, args any) {
	cl, err := mcp.Dial(shell.DefaultSocket(), shell.RoleAgent, "ctl")
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	var out any
	cctx, cancel := context.WithTimeout(ctx, 11*time.Minute)
	defer cancel()
	if err := cl.Call(cctx, op, args, &out); err != nil {
		log.Fatal(err)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}

func daemon(ctx context.Context) error {
	cfg := filepath.Join(configDir(), "basalt-shell")
	store := theme.NewStore(themeDirs(), cfg)
	if err := store.Load(); err != nil {
		return err
	}
	for _, e := range store.LoadErrors() {
		log.Printf("theme: %s", e)
	}
	lg, err := audit.Open(filepath.Join(stateDir(), "audit.jsonl"))
	if err != nil {
		return err
	}
	comp := compositor.Detect()
	rep := hw.Probe()
	core := shell.New(comp, store, lg, rep, configDir())
	if os.Getenv("BASALT_SHELL_NO_APPS") == "1" {
		core.ApplyApps = false
	}
	core.Translator = intent.FromAssistantConfig("/etc/basalt/assistant.conf")
	if b, ok := assistant.Default(); ok {
		core.Assistant = b
	}
	log.Printf("compositor %s %s; hardware weak=%v %v; translator=%v; assistant=%v",
		comp.Name(), comp.Version(ctx), rep.Weak, rep.Reasons, core.Translator != nil, core.Assistant != nil)
	_, _ = lg.Append("start", "daemon", "basalt-shell "+version+" on "+comp.Name(), map[string]any{"hardware": rep})
	core.Refresh(ctx)
	core.ApplyTheme(ctx)
	go core.Watch(ctx)
	srv := &shell.Server{Core: core, Path: shell.DefaultSocket(), UIExecutables: []string{"quickshell", "qs"},
		InsecureUI: os.Getenv("BASALT_SHELL_INSECURE_UI") == "1"}
	log.Printf("listening on %s", srv.Path)
	return srv.Listen(ctx)
}
