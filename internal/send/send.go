// Package send delivers a message over SMTP with implicit TLS or STARTTLS.
// It greets the server as "localhost" so the Mac's host name never leaves it.
package send

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"time"

	"github.com/jamalxcode/laser-email/internal/provider"
)

// Credentials for SMTP: a password (PLAIN) or an OAuth access token (XOAUTH2).
type Credentials struct {
	User        string
	Password    string
	OAuthBearer string
}

type xoauth2 struct{ user, token string }

func (a xoauth2) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"), nil
}

func (a xoauth2) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return []byte{}, nil // the server sent an error; end the exchange
	}
	return nil, nil
}

// plain is PLAIN auth that, unlike smtp.PlainAuth, also works with a
// loopback server without TLS (tests, local bridges). provider.CheckSecurity
// has already refused plain text to anything else.
type plain struct{ user, pass string }

func (a plain) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a plain) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

// Send delivers raw to the recipients.
func Send(ctx context.Context, srv provider.Server, cred Credentials, from string, to []string, raw []byte) error {
	if err := provider.CheckSecurity(srv); err != nil {
		return err
	}
	if len(to) == 0 {
		return errors.New("no recipients")
	}
	d := &net.Dialer{Timeout: 20 * time.Second}
	tlsConf := &tls.Config{ServerName: srv.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if srv.Security == provider.TLS {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tlsConf}).DialContext(ctx, "tcp", srv.Addr())
	} else {
		conn, err = d.DialContext(ctx, "tcp", srv.Addr())
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", srv.Host, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	} else {
		conn.SetDeadline(time.Now().Add(2 * time.Minute))
	}
	c, err := smtp.NewClient(conn, srv.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if srv.Security == provider.StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS; refusing to send in plain text", srv.Host)
		}
		if err := c.StartTLS(tlsConf); err != nil {
			return err
		}
	}
	var auth smtp.Auth
	if cred.OAuthBearer != "" {
		auth = xoauth2{cred.User, cred.OAuthBearer}
	} else {
		auth = plain{cred.User, cred.Password}
	}
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("sign in to %s: %w", srv.Host, err)
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("recipient %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
