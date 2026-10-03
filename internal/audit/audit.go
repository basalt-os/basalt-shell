// Package audit is the shell's append-only, hash-chained activity log
// (one JSON record per line), in the same format family as the system
// assistant's audit log: each record carries the previous record's hash
// and its own, so editing, removing or reordering a record breaks the
// chain. It lives in the user's state directory: it records what agents
// asked the desktop to do and what the person decided.
package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is one entry.
type Record struct {
	Seq   int64          `json:"seq"`
	Time  time.Time      `json:"time"`
	Type  string         `json:"type"`  // request, confirm, decline, expire, apply, fail, ask, start
	Actor string         `json:"actor"` // ui, mcp:<client>, ipc:<client>, commandbar, assistant
	Text  string         `json:"text"`
	Data  map[string]any `json:"data,omitempty"`
	Prev  string         `json:"prev"`
	Hash  string         `json:"hash"`
}

// Log is an open audit log.
type Log struct {
	path string
	mu   sync.Mutex
	seq  int64
	prev string
	tail []Record
	// OnAppend is called (outside the lock) after each record is written.
	OnAppend func(Record)
}

const tailSize = 200

// Open reads the existing chain (to continue it) and keeps the last
// records in memory for the activity feed.
func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	l := &Log{path: path}
	f, err := os.Open(path)
	if err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var r Record
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue
			}
			l.seq, l.prev = r.Seq, r.Hash
			l.tail = append(l.tail, r)
			if len(l.tail) > tailSize {
				l.tail = l.tail[1:]
			}
		}
		f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return l, nil
}

func hashOf(r Record) string {
	r.Hash = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Append writes a record.
func (l *Log) Append(typ, actor, text string, data map[string]any) (Record, error) {
	// Canonical form: what a reader will decode (maps with sorted keys),
	// so that the hash can be recomputed from the file.
	if data != nil {
		if b, err := json.Marshal(data); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				data = m
			}
		}
	}
	l.mu.Lock()
	r := Record{Seq: l.seq + 1, Time: time.Now().UTC().Truncate(time.Millisecond), Type: typ, Actor: actor, Text: text, Data: data, Prev: l.prev}
	r.Hash = hashOf(r)
	b, err := json.Marshal(r)
	if err != nil {
		l.mu.Unlock()
		return r, err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		l.mu.Unlock()
		return r, err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		l.mu.Unlock()
		return r, err
	}
	l.seq, l.prev = r.Seq, r.Hash
	l.tail = append(l.tail, r)
	if len(l.tail) > tailSize {
		l.tail = l.tail[1:]
	}
	cb := l.OnAppend
	l.mu.Unlock()
	if cb != nil {
		cb(r)
	}
	return r, nil
}

// Tail returns up to n recent records, newest last.
func (l *Log) Tail(n int) []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.tail) {
		n = len(l.tail)
	}
	return append([]Record(nil), l.tail[len(l.tail)-n:]...)
}

// Verify walks the whole file and checks the chain.
func Verify(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var prev string
	var n int64
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return n, fmt.Errorf("line %d: %v", n+1, err)
		}
		if r.Prev != prev {
			return n, fmt.Errorf("record %d: chain broken (prev hash mismatch)", r.Seq)
		}
		if hashOf(r) != r.Hash {
			return n, fmt.Errorf("record %d: hash mismatch (edited)", r.Seq)
		}
		prev = r.Hash
		n++
	}
	return n, sc.Err()
}
