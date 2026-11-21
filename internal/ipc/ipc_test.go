package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	dir, _ := os.MkdirTemp("", "lsr") // short path: socket names are limited to ~100 bytes
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")
	s := &Server{
		Handle: func(ctx context.Context, op string, args json.RawMessage) (any, error) {
			if op == "echo" {
				var v map[string]any
				json.Unmarshal(args, &v)
				return v, nil
			}
			return nil, errors.New("unknown op " + op)
		},
		Subscribe: func(ctx context.Context, send func(any)) { send(map[string]string{"type": "hello"}) },
	}
	if err := s.Listen(sock); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Serve(ctx)
	if fi, _ := os.Stat(sock); fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", fi.Mode().Perm())
	}
	if err := (&Server{}).Listen(sock); err == nil {
		t.Fatal("second daemon allowed")
	}
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var out map[string]any
	if err := c.Call(ctx, "echo", map[string]int{"a": 1}, &out); err != nil || out["a"].(float64) != 1 {
		t.Fatalf("%v %v", out, err)
	}
	if err := c.Call(ctx, "nope", nil, nil); err == nil || err.Error() != "unknown op nope" {
		t.Fatalf("error: %v", err)
	}
	c.Call(ctx, "subscribe", nil, nil)
	select {
	case ev := <-c.Events:
		if string(ev) != `{"type":"hello"}` {
			t.Fatalf("event %s", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
}
