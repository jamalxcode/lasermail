// Package ipc is the protocol between the app (or CLI) and laserd: JSON
// lines over a Unix socket that only the user can open. Requests carry an
// id; responses echo it. A connection that sends "subscribe" also receives
// {"event":…} lines.
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Version of the protocol; bump on incompatible changes.
const Version = 1

// Request from a client.
type Request struct {
	ID   int64           `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response from the daemon. Exactly one of Result and Error is set, except
// for event lines, which carry only Event.
type Response struct {
	ID     int64           `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Event  any             `json:"event,omitempty"`
}

// Handler answers one request.
type Handler func(ctx context.Context, op string, args json.RawMessage) (any, error)

// Subscriber yields events until ctx ends.
type Subscriber func(ctx context.Context, send func(any))

// Server listens on a Unix socket.
type Server struct {
	Handle    Handler
	Subscribe Subscriber
	ln        net.Listener
}

// Listen creates the socket (replacing a stale one) with 0600 permissions.
func (s *Server) Listen(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return fmt.Errorf("laserd is already running (%s)", path)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}
	s.ln = ln
	return nil
}

// Serve accepts connections until ctx ends.
func (s *Server) Serve(ctx context.Context) error {
	go func() { <-ctx.Done(); s.ln.Close() }()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.conn(ctx, c)
	}
}

func (s *Server) conn(ctx context.Context, c net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer c.Close()
	var wmu sync.Mutex
	enc := json.NewEncoder(c)
	write := func(r Response) {
		wmu.Lock()
		enc.Encode(r)
		wmu.Unlock()
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64<<10), 64<<20) // drafts with attachments
	var subscribed atomic.Bool
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			write(Response{Error: "bad request: " + err.Error()})
			continue
		}
		if req.Op == "subscribe" {
			if s.Subscribe != nil && !subscribed.Swap(true) {
				go s.Subscribe(ctx, func(ev any) { write(Response{Event: ev}) })
			}
			write(Response{ID: req.ID, Result: json.RawMessage(`true`)})
			continue
		}
		go func(req Request) {
			res, err := s.Handle(ctx, req.Op, req.Args)
			if err != nil {
				write(Response{ID: req.ID, Error: err.Error()})
				return
			}
			b, err := json.Marshal(res)
			if err != nil {
				write(Response{ID: req.ID, Error: err.Error()})
				return
			}
			write(Response{ID: req.ID, Result: b})
		}(req)
	}
}

// Client talks to the daemon.
type Client struct {
	c       net.Conn
	enc     *json.Encoder
	wmu     sync.Mutex
	next    atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan Response
	Events  chan json.RawMessage
	done    chan struct{}
}

// Dial connects to the daemon's socket.
func Dial(path string) (*Client, error) {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("laserd is not running (%w)", err)
	}
	cl := &Client{c: c, enc: json.NewEncoder(c), pending: map[int64]chan Response{}, Events: make(chan json.RawMessage, 64), done: make(chan struct{})}
	go cl.read()
	return cl, nil
}

func (cl *Client) read() {
	defer close(cl.done)
	dec := json.NewDecoder(cl.c)
	for {
		var raw struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
			Event  json.RawMessage `json:"event"`
		}
		if err := dec.Decode(&raw); err != nil {
			cl.mu.Lock()
			for _, ch := range cl.pending {
				ch <- Response{Error: "connection to laserd lost"}
			}
			cl.pending = nil
			cl.mu.Unlock()
			close(cl.Events)
			return
		}
		if raw.Event != nil {
			select {
			case cl.Events <- raw.Event:
			default:
			}
			continue
		}
		cl.mu.Lock()
		ch := cl.pending[raw.ID]
		delete(cl.pending, raw.ID)
		cl.mu.Unlock()
		if ch != nil {
			ch <- Response{ID: raw.ID, Result: raw.Result, Error: raw.Error}
		}
	}
}

// Call sends a request and decodes the result into out (may be nil).
func (cl *Client) Call(ctx context.Context, op string, args, out any) error {
	id := cl.next.Add(1)
	ch := make(chan Response, 1)
	cl.mu.Lock()
	if cl.pending == nil {
		cl.mu.Unlock()
		return errors.New("connection to laserd lost")
	}
	cl.pending[id] = ch
	cl.mu.Unlock()
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return err
		}
		raw = b
	}
	cl.wmu.Lock()
	err := cl.enc.Encode(Request{ID: id, Op: op, Args: raw})
	cl.wmu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.Error != "" {
			return errors.New(r.Error)
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	}
}

// Close disconnects.
func (cl *Client) Close() error { return cl.c.Close() }
