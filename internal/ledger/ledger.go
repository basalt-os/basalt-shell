// Package ledger sends the shell's security-relevant records to
// basalt-ledger (ADR 0010), the system's append-only audit trail, as the
// "basalt-shell" producer: proposals of acting skills and their outcome
// (confirmed, declined, expired, failed, edited), the exact preview of
// what was done, grants, refusals and skill sessions. The shell's own
// activity log stays the first record (hash-chained, shown in the
// timeline); the ledger copy is the one the shell cannot rewrite.
//
// Delivery is best effort and never blocks the shell: records wait in a
// bounded queue and are dropped (and counted) when the ledger is not
// running.
package ledger

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"sync"
	"time"
)

// DefaultSocket is basalt-ledger's socket.
const DefaultSocket = "/run/basalt-ledger/ledger.sock"

// Sink forwards records to the ledger.
type Sink struct {
	Socket string
	q      chan map[string]any
	mu     sync.Mutex
	// Dropped counts records that could not be delivered.
	Dropped int
	Sent    int
}

// New starts a sink (nil when the ledger socket does not exist).
func New(socket string) *Sink {
	if socket == "" {
		socket = DefaultSocket
	}
	if _, err := os.Stat(socket); err != nil {
		return nil
	}
	s := &Sink{Socket: socket, q: make(chan map[string]any, 512)}
	go s.loop()
	return s
}

// Append queues one record: event (a name like shell.done), outcome (ok,
// allowed, denied, error), an optional skill session id and data.
func (s *Sink) Append(event, outcome, session string, data map[string]any) {
	if s == nil {
		return
	}
	uid := os.Getuid()
	rec := map[string]any{"v": 1, "time": time.Now().UTC().Format(time.RFC3339Nano), "producer": "basalt-shell", "uid": uid,
		"event": event, "outcome": outcome, "subject": map[string]any{"app": "Basalt desktop assistant"}, "data": data}
	if session != "" {
		rec["session"] = session
	}
	select {
	case s.q <- rec:
	default:
		s.mu.Lock()
		s.Dropped++
		s.mu.Unlock()
	}
}

func (s *Sink) loop() {
	var c net.Conn
	var br *bufio.Reader
	for rec := range s.q {
		ok := false
		for try := 0; try < 2 && !ok; try++ {
			if c == nil {
				var err error
				c, err = net.DialTimeout("unix", s.Socket, 2*time.Second)
				if err != nil {
					c = nil
					break
				}
				br = bufio.NewReader(c)
			}
			b, _ := json.Marshal(map[string]any{"op": "append", "record": rec})
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write(append(b, '\n')); err != nil {
				c.Close()
				c = nil
				continue
			}
			line, err := br.ReadBytes('\n')
			if err != nil {
				c.Close()
				c = nil
				continue
			}
			var rep struct {
				OK bool `json:"ok"`
			}
			ok = json.Unmarshal(line, &rep) == nil && rep.OK
			break
		}
		s.mu.Lock()
		if ok {
			s.Sent++
		} else {
			s.Dropped++
		}
		s.mu.Unlock()
	}
}
