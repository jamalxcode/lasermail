// Package imapsync keeps the local store in step with an IMAP account:
// folders, new mail, flag changes and deletions, plus the actions the user
// takes (read, flag, move, delete) and IDLE for push.
package imapsync

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"

	"github.com/jamalxcode/laser-email/internal/provider"
)

// Credentials log a connection in. Exactly one of Password and SASL is used.
type Credentials struct {
	User     string
	Password string
	SASL     sasl.Client
}

// Conn is one logged-in IMAP connection.
type Conn struct {
	c       *imapclient.Client
	changed chan struct{} // signalled when the server reports a change (IDLE)
}

// ErrAuth is returned when the server refuses the login.
var ErrAuth = errors.New("the server refused the user name or password")

// Dial connects and logs in.
func Dial(ctx context.Context, srv provider.Server, cred Credentials) (*Conn, error) {
	if err := provider.CheckSecurity(srv); err != nil {
		return nil, err
	}
	conn := &Conn{changed: make(chan struct{}, 1)}
	notify := func() {
		select {
		case conn.changed <- struct{}{}:
		default:
		}
	}
	opts := &imapclient.Options{
		TLSConfig:   &tls.Config{ServerName: srv.Host, MinVersion: tls.VersionTLS12},
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
		Dialer:      &net.Dialer{Timeout: 20 * time.Second},
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Expunge: func(uint32) { notify() },
			Mailbox: func(d *imapclient.UnilateralDataMailbox) {
				if d.NumMessages != nil {
					notify()
				}
			},
			Fetch: func(*imapclient.FetchMessageData) { notify() },
		},
	}
	var c *imapclient.Client
	var err error
	switch srv.Security {
	case provider.TLS:
		c, err = imapclient.DialTLS(srv.Addr(), opts)
	case provider.StartTLS:
		c, err = imapclient.DialStartTLS(srv.Addr(), opts)
	default:
		c, err = imapclient.DialInsecure(srv.Addr(), opts)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", srv.Host, err)
	}
	conn.c = c
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if cred.SASL != nil {
		err = c.Authenticate(cred.SASL)
	} else {
		err = c.Login(cred.User, cred.Password).Wait()
	}
	if err != nil {
		c.Close()
		var ie *imap.Error
		if errors.As(err, &ie) && ie.Type == imap.StatusResponseTypeNo {
			return nil, fmt.Errorf("%w (%s)", ErrAuth, ie.Text)
		}
		return nil, fmt.Errorf("login: %w", err)
	}
	// Identify as nothing in particular: no ID command is ever sent.
	return conn, nil
}

// Close logs out.
func (c *Conn) Close() error {
	c.c.Logout().Wait()
	return c.c.Close()
}

// Closed is closed when the connection drops.
func (c *Conn) Closed() <-chan struct{} { return c.c.Closed() }

// Idle waits in IDLE on the selected folder until the server reports a
// change, ctx ends, or 25 minutes pass (servers drop IDLE after 30).
// It answers true when something changed.
func (c *Conn) Idle(ctx context.Context) (bool, error) {
	if !c.c.Caps().Has(imap.CapIdle) {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(2 * time.Minute): // poll
			return true, nil
		}
	}
	cmd, err := c.c.Idle()
	if err != nil {
		return false, err
	}
	changed := false
	select {
	case <-ctx.Done():
	case <-c.changed:
		changed = true
	case <-time.After(25 * time.Minute):
	case <-c.c.Closed():
		return false, errors.New("connection closed")
	}
	if err := cmd.Close(); err != nil {
		return changed, err
	}
	return changed, cmd.Wait()
}
