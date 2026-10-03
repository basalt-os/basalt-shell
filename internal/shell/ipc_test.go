package shell

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
)

type testClient struct {
	c  net.Conn
	r  *bufio.Reader
	id int64
}

func dialT(t *testing.T, path, _ string) *testClient {
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &testClient{c: c, r: bufio.NewReader(c)}
}

func (tc *testClient) call(op string, args any) Reply {
	tc.id++
	raw, _ := json.Marshal(args)
	b, _ := json.Marshal(Request{ID: tc.id, Op: op, Args: raw})
	tc.c.Write(append(b, '\n'))
	for {
		line, err := tc.r.ReadBytes('\n')
		if err != nil {
			return Reply{Error: err.Error()}
		}
		var rep Reply
		if json.Unmarshal(line, &rep) == nil && rep.ID == tc.id {
			return rep
		}
	}
}
