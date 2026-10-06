// fake-greetd: a greetd stand-in for running the login screen without
// PAM (lab and development): users and passwords from the command line,
// greetd 0.10's answers (internal/greetd.Fake). Prints each started
// session; never prints an answer.
//
//	go run ./lab/greeter/fake-greetd -sock /tmp/greetd.sock -user ana=secret -user basalt=staple
//	GREETD_SOCK=/tmp/greetd.sock quickshell -p greeter
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/basalt-os/basalt-shell/internal/greetd"
)

type users map[string]string

func (u users) String() string { return fmt.Sprint(len(u), " users") }
func (u users) Set(v string) error {
	name, pass, ok := strings.Cut(v, "=")
	if !ok || name == "" {
		return fmt.Errorf("want NAME=PASSWORD")
	}
	u[name] = pass
	return nil
}

func main() {
	us := users{}
	sock := flag.String("sock", "/tmp/fake-greetd.sock", "socket path")
	notice := flag.String("notice", "", "PAM notice sent before each refused password")
	locked := flag.String("locked", "", "comma-separated locked users")
	flag.Var(us, "user", "NAME=PASSWORD (repeat)")
	flag.Parse()
	f := &greetd.Fake{Users: us, Notice: *notice, Locked: map[string]bool{}}
	for _, l := range strings.Split(*locked, ",") {
		if l != "" {
			f.Locked[l] = true
		}
	}
	if err := f.Listen(*sock); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("fake greetd on %s (%d users)\n", *sock, len(us))
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	seen := 0
	for {
		select {
		case <-stop:
			_ = f.Close()
			return
		case <-time.After(200 * time.Millisecond):
			s := f.Sessions()
			for ; seen < len(s); seen++ {
				fmt.Printf("session started: user=%s cmd=%q env=%q\n", s[seen].User, s[seen].Cmd, s[seen].Env)
			}
		}
	}
}
