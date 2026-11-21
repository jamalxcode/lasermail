package send

import (
	"bufio"
	"encoding/base64"
	"net"
	"strings"
	"sync"
)

// FakeServer is a tiny SMTP server for tests: it accepts PLAIN or XOAUTH2
// with the given secret and records what it received.
type FakeServer struct {
	Addr   string
	Secret string

	mu   sync.Mutex
	Mail []Received
	ln   net.Listener
}

// Received is one delivered message.
type Received struct {
	Helo, From string
	To         []string
	Data       string
}

// NewFakeServer listens on 127.0.0.1.
func NewFakeServer(secret string) (*FakeServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &FakeServer{Addr: ln.Addr().String(), Secret: secret, ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s, nil
}

func (s *FakeServer) Close() { s.ln.Close() }

// Received returns what was delivered so far.
func (s *FakeServer) Received() []Received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Received(nil), s.Mail...)
}

func (s *FakeServer) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(l string) { c.Write([]byte(l + "\r\n")) }
	say("220 fake ESMTP")
	var cur Received
	authed := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			cur.Helo = strings.TrimSpace(line[4:])
			say("250-fake")
			say("250 AUTH PLAIN XOAUTH2")
		case strings.HasPrefix(up, "AUTH "):
			f := strings.Fields(line)
			b, _ := base64.StdEncoding.DecodeString(f[len(f)-1])
			if strings.HasSuffix(string(b), "\x00"+s.Secret) || strings.Contains(string(b), "auth=Bearer "+s.Secret+"\x01") {
				authed = true
				say("235 ok")
			} else {
				say("535 bad credentials")
			}
		case strings.HasPrefix(up, "MAIL FROM:"):
			if !authed {
				say("530 auth first")
				continue
			}
			cur.From = strings.Trim(line[10:], "<> ")
			say("250 ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			cur.To = append(cur.To, strings.Trim(line[8:], "<> "))
			say("250 ok")
		case up == "DATA":
			say("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			cur.Data = b.String()
			s.mu.Lock()
			s.Mail = append(s.Mail, cur)
			s.mu.Unlock()
			cur = Received{Helo: cur.Helo}
			say("250 queued")
		case up == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}
