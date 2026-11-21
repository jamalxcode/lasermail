package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamalxcode/lasermail/internal/engine"
	"github.com/jamalxcode/lasermail/internal/ipc"
	"github.com/jamalxcode/lasermail/internal/secret"
	"github.com/jamalxcode/lasermail/internal/store"
)

// The demo data through the real protocol: what the app sees.
func TestDemoOverSocket(t *testing.T) {
	dir, _ := os.MkdirTemp("", "lsrd")
	defer os.RemoveAll(dir)
	e, err := engine.New(engine.Config{DataDir: dir, Secrets: secret.NewMemory(), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.SeedDemo(time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := Server(e, false)
	sock := filepath.Join(dir, "s")
	if err := srv.Listen(sock); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)
	c, err := ipc.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var hello map[string]any
	if err := c.Call(ctx, "hello", nil, &hello); err != nil || hello["protocol"].(float64) != ipc.Version {
		t.Fatalf("hello %v %v", hello, err)
	}
	var accts struct {
		Accounts []store.Account `json:"accounts"`
	}
	c.Call(ctx, "accounts", nil, &accts)
	if len(accts.Accounts) != 2 {
		t.Fatalf("accounts %+v", accts)
	}
	var inbox []store.Message
	c.Call(ctx, "list", store.Query{Role: store.Inbox}, &inbox)
	if len(inbox) != 9 || inbox[0].Subject != "Q4 roadmap review — moved to Thursday" {
		t.Fatalf("inbox %d %q", len(inbox), inbox[0].Subject)
	}
	var counts map[string]int
	c.Call(ctx, "counts", nil, &counts)
	if counts["inbox"] != 4 || counts["flagged"] != 2 {
		t.Fatalf("counts %v", counts)
	}
	var sale store.Message
	for _, m := range inbox {
		if strings.HasPrefix(m.Subject, "Your weekend") {
			sale = m
		}
	}
	var v struct {
		HTML    string `json:"html"`
		Privacy struct {
			Trackers []string `json:"trackers"`
		} `json:"privacy"`
		Seen bool `json:"seen"`
	}
	// Offline: marking read fails (no server), opening still works.
	if err := c.Call(ctx, "open", map[string]any{"id": sale.ID}, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Privacy.Trackers) != 2 || strings.Contains(v.HTML, "utm_") {
		t.Fatalf("demo newsletter: %+v", v.Privacy)
	}
	var raw json.RawMessage
	if err := c.Call(ctx, "bogus", nil, &raw); err == nil {
		t.Fatal("unknown op accepted")
	}
}
