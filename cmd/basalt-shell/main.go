// Command basalt-shell is the client side of the Basalt desktop shell:
// the MCP server for agents and the command line for scripts. It talks to
// the daemon (basalt-shelld) over the local socket with the agent role:
// it can read and request, never confirm. With the Basalt SELinux policy
// it runs confined in basalt_agent_mcp_t.
//
//	basalt-shell mcp               MCP server on stdio (for a local model or an MCP client)
//	basalt-shell ctl OP [JSON]     send one IPC request and print the reply
//	basalt-shell propose ACTION [JSON-ARGS] [--wait N]
//	                               propose one typed action; the person confirms it in the shell
//	basalt-shell screenshot [--window REF | --output NAME] [--max-width N] [FILE.png|-]
//	                               ask for a screenshot (confirmed by the person); PNG on stdout by default
//	basalt-shell choose-output     ask the person which screen to share (portal chooser)
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
	"strconv"
	"syscall"
	"time"

	"github.com/openbasalt/basalt-shell/internal/audit"
	"github.com/openbasalt/basalt-shell/internal/mcp"
	"github.com/openbasalt/basalt-shell/internal/paths"
	"github.com/openbasalt/basalt-shell/internal/shell"
)

var version = "0.2.0-dev"

func usage() {
	fmt.Fprint(os.Stderr, `usage: basalt-shell mcp | ctl OP [JSON] | propose ACTION [JSON] [--wait N] | screenshot [--window REF|--output NAME] FILE.png | choose-output | audit verify | version
(the daemon is basalt-shelld)
`)
	os.Exit(2)
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
		log.Fatal("the daemon is basalt-shelld")
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
	case "choose-output":
		// Screen-share output chooser for xdg-desktop-portal-wlr
		// (chooser_cmd): prints the output the person picked in the shell.
		chooseOutput(ctx)
	case "audit":
		auditVerify(ctx)
	case "screenshot":
		screenshot(ctx, os.Args[2:])
	case "version":
		fmt.Println("basalt-shell", version)
	default:
		usage()
	}
}

func ctl(ctx context.Context, op string, args any) {
	// BASALT_SHELL_ROLE=ui is only honored by a daemon started with
	// BASALT_SHELL_INSECURE_UI=1 (development and lab tests).
	role := shell.RoleAgent
	if os.Getenv("BASALT_SHELL_ROLE") == shell.RoleUI {
		role = shell.RoleUI
	}
	cl, err := mcp.Dial(shell.DefaultSocket(), role, "ctl")
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

func chooseOutput(ctx context.Context) {
	cl, err := mcp.Dial(shell.DefaultSocket(), shell.RoleAgent, "screen-share")
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	var d shell.Desktop
	if err := cl.Call(ctx, "desktop", nil, &d); err != nil {
		log.Fatal(err)
	}
	var opts []shell.Option
	for _, o := range d.Outputs {
		opts = append(opts, shell.Option{ID: o.Name, Label: o.Name, Hint: fmt.Sprintf("%dx%d", o.Rect.W, o.Rect.H)})
	}
	var res struct {
		Choice string `json:"choice"`
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if err := cl.Call(cctx, "choose", map[string]any{"title": "Share your screen", "body": "An application wants to see your screen. Pick what to share, or cancel.", "options": opts, "wait": 120}, &res); err != nil {
		log.Fatal(err)
	}
	if res.Choice == "" {
		os.Exit(1)
	}
	fmt.Println(res.Choice)
}

// auditVerify asks the daemon to check the activity log's chain (the
// log is the daemon's: a confined client does not read it), or checks
// the file itself when no daemon runs.
func auditVerify(ctx context.Context) {
	var res struct {
		OK      bool   `json:"ok"`
		Records int64  `json:"records"`
		Error   string `json:"error"`
	}
	cl, err := mcp.Dial(shell.DefaultSocket(), shell.RoleAgent, "ctl")
	if err == nil {
		defer cl.Close()
		err = cl.Call(ctx, "audit.verify", nil, &res)
	}
	if err != nil {
		n, verr := audit.Verify(paths.AuditLog())
		res.Records, res.OK = n, verr == nil
		if verr != nil {
			res.Error = verr.Error()
		}
	}
	if !res.OK {
		fmt.Printf("audit chain BROKEN after %d records: %s\n", res.Records, res.Error)
		os.Exit(1)
	}
	fmt.Printf("audit chain ok: %d records\n", res.Records)
}

// screenshot asks the daemon for a screenshot and writes the PNG.
func screenshot(ctx context.Context, argv []string) {
	args := map[string]any{}
	file := ""
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--window", "--output", "--max-width":
			if i+1 >= len(argv) {
				usage()
			}
			v := argv[i+1]
			i++
			switch argv[i-1] {
			case "--window":
				args["target"], args["window"] = "window", v
			case "--output":
				args["target"], args["output"] = "output", v
			default:
				n, _ := strconv.Atoi(v)
				args["max_width"] = n
			}
		default:
			file = argv[i]
		}
	}
	if file == "" {
		file = "-"
	}
	cl, err := mcp.Dial(shell.DefaultSocket(), shell.RoleAgent, "ctl")
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	var res shell.CaptureResult
	cctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	if err := cl.Call(cctx, "capture", map[string]any{"args": args, "wait": 180}, &res); err != nil {
		log.Fatal(err)
	}
	if len(res.PNG) == 0 {
		log.Fatalf("no screenshot: %s", res.Status)
	}
	// Confined (basalt_agent_mcp_t) this program cannot create files:
	// "-" (the default) writes to stdout for the caller to redirect.
	if file == "-" {
		os.Stdout.Write(res.PNG)
	} else if err := os.WriteFile(file, res.PNG, 0o600); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "screenshot %dx%d (%s)\n", res.Capture.Width, res.Capture.Height, res.Capture.Method)
}
