package mailout

import (
	"bytes"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestCompose(t *testing.T) {
	d := Draft{FromName: "Alex Morgan", From: "alex@example.com", To: "ana.souza@example.org", ToName: "Ana Souza",
		Subject: "Re: Almoço na quinta?", Body: "Hi Ana,\n\nThursday works for me.\n\nAlex", InReplyTo: "<b1@example.org>"}
	raw, id, err := Compose(d, time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.Header.Get("Message-Id") != id || m.Header.Get("In-Reply-To") != "<b1@example.org>" || m.Header.Get("References") != "<b1@example.org>" {
		t.Errorf("headers: %v", m.Header)
	}
	if to, _ := mail.ParseAddress(m.Header.Get("To")); to.Address != "ana.souza@example.org" {
		t.Errorf("to %v", to)
	}
	if !strings.Contains(m.Header.Get("Content-Type"), "text/plain") {
		t.Error("not plain text")
	}
	if strings.Contains(string(raw), "\r\r") {
		t.Error("double carriage return")
	}
}

func TestCheckRefuses(t *testing.T) {
	ok := Draft{From: "alex@example.com", To: "ana@example.org", Subject: "Re: x", Body: "hi"}
	bad := []Draft{
		{From: "alex@example.com", To: "ana@example.org, eve@example.net", Subject: "x", Body: "hi"},
		{From: "alex@example.com", To: "ana@example.org", Subject: "x\r\nBcc: eve@example.net", Body: "hi"},
		{From: "alex@example.com", To: "Ana <ana@example.org>", Subject: "x", Body: "hi"},
		{From: "alex@example.com", To: "ana@example.org", Subject: "x", Body: "  "},
		{From: "alex@example.com", To: "ana@example.org", Subject: "x", Body: "hi", InReplyTo: "<a> <b>"},
	}
	if err := ok.Check(); err != nil {
		t.Errorf("ok draft refused: %v", err)
	}
	for i, d := range bad {
		if d.Check() == nil {
			t.Errorf("bad draft %d accepted", i)
		}
	}
}
