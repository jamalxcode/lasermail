package send

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/jamalxcode/lasermail/internal/provider"
)

func server(t *testing.T) (*FakeServer, provider.Server) {
	s, err := NewFakeServer("pw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	h, p, _ := net.SplitHostPort(s.Addr)
	port, _ := strconv.Atoi(p)
	return s, provider.Server{Host: h, Port: port, Security: provider.Insecure}
}

func TestSend(t *testing.T) {
	s, srv := server(t)
	raw := []byte("Subject: x\r\n\r\n.dot line\r\nbody\r\n")
	if err := Send(context.Background(), srv, Credentials{User: "me", Password: "pw"}, "me@x.com", []string{"a@y.com", "b@y.com"}, raw); err != nil {
		t.Fatal(err)
	}
	got := s.Received()
	if len(got) != 1 || got[0].From != "me@x.com" || len(got[0].To) != 2 || got[0].Helo != "localhost" || !strings.Contains(got[0].Data, ".dot line") {
		t.Fatalf("%+v", got)
	}
	if err := Send(context.Background(), srv, Credentials{User: "me", OAuthBearer: "pw"}, "me@x.com", []string{"a@y.com"}, raw); err != nil {
		t.Fatalf("xoauth2: %v", err)
	}
	if err := Send(context.Background(), srv, Credentials{User: "me", Password: "wrong"}, "me@x.com", []string{"a@y.com"}, raw); err == nil {
		t.Fatal("bad password accepted")
	}
}

func TestRefusesPlainTextRemote(t *testing.T) {
	err := Send(context.Background(), provider.Server{Host: "smtp.example.org", Port: 25, Security: provider.Insecure}, Credentials{}, "a@b", []string{"c@d"}, nil)
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("%v", err)
	}
}
