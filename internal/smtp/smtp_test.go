package smtp

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeServer accepts one session and records the commands and the data.
func fakeServer(t *testing.T) (string, chan []string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan []string, 1)
	go func() {
		c, err := l.Accept()
		l.Close()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		var log []string
		w("220 lab ESMTP")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				got <- log
				return
			}
			line = strings.TrimRight(line, "\r\n")
			log = append(log, line)
			if inData {
				if line == "." {
					inData = false
					w("250 2.0.0 queued as ABC123")
				}
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				w("250-lab")
				w("250-AUTH PLAIN")
				w("250 SIZE 1000000")
			case strings.HasPrefix(line, "AUTH PLAIN"):
				w("235 ok")
			case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				w("250 ok")
			case line == "DATA":
				inData = true
				w("354 go")
			case line == "QUIT":
				w("221 bye")
				got <- log
				return
			default:
				w("502 no")
			}
		}
	}()
	return l.Addr().String(), got
}

func TestSendLab(t *testing.T) {
	addr, got := fakeServer(t)
	raw := []byte("From: a@example.com\r\nTo: b@example.org\r\nSubject: x\r\n\r\n.hidden dot line\r\nbye\r\n")
	res, err := Send(Options{Addr: addr, TLS: "none", User: "a", Pass: "p", Timeout: 5 * time.Second}, "a@example.com", "b@example.org", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Reply, "ABC123") {
		t.Errorf("reply %q", res.Reply)
	}
	log := <-got
	joined := strings.Join(log, "\n")
	if !strings.Contains(joined, "RCPT TO:<b@example.org>") || strings.Count(joined, "RCPT") != 1 {
		t.Errorf("recipients: %s", joined)
	}
	if !strings.Contains(joined, "\n..hidden dot line") {
		t.Errorf("dot stuffing: %s", joined)
	}
}

func TestStartTLSRequired(t *testing.T) {
	addr, _ := fakeServer(t)
	_, err := Send(Options{Addr: addr, Timeout: 5 * time.Second}, "a@example.com", "b@example.org", []byte("x\r\n"))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("plain server accepted without TLS: %v", err)
	}
}

func TestClosedCommandSet(t *testing.T) {
	sc := &Conn{}
	for _, v := range []string{"VRFY", "EXPN", "TURN", "ETRN"} {
		if _, _, err := sc.cmd(v, ""); err == nil {
			t.Errorf("%s allowed", v)
		}
	}
	if _, err := Send(Options{Addr: "127.0.0.1:1", TLS: "none"}, "a@example.com", "b@example.org, c@example.net", nil); err == nil {
		t.Error("two recipients accepted")
	}
}
