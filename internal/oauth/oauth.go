// Package oauth signs in to Google with OAuth 2.0 for installed apps: the
// browser opens Google's consent page, Google redirects back to a one-shot
// listener on 127.0.0.1, and the code is exchanged with PKCE. Only the
// https://mail.google.com/ scope is requested (IMAP and SMTP need it).
//
// This package and the mail connections are the only code that talks to the
// network; internal/netcheck enforces that.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config is a Google OAuth client ("Desktop app" type). For installed apps
// the client secret is not confidential (Google documents this).
type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AuthURL      string `json:"-"` // overridable for tests
	TokenURL     string `json:"-"`
}

const (
	GoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
	GmailScope     = "https://mail.google.com/"
	// Scopes asked for: Gmail over IMAP/SMTP, plus the account's address
	// (OpenID "email"), so the user picks an account instead of typing it.
	Scopes = GmailScope + " openid email"
)

func (c Config) authURL() string {
	if c.AuthURL != "" {
		return c.AuthURL
	}
	return GoogleAuthURL
}

func (c Config) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return GoogleTokenURL
}

// Token is what is kept in the Keychain.
type Token struct {
	Access  string    `json:"access_token"`
	Refresh string    `json:"refresh_token"`
	Expiry  time.Time `json:"expiry"`
	// Email is the Google account the user chose (from the ID token).
	Email string `json:"email,omitempty"`
}

// Valid: usable for at least another minute.
func (t *Token) Valid() bool { return t.Access != "" && time.Until(t.Expiry) > time.Minute }

var client = &http.Client{Timeout: 30 * time.Second}

func random(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Authorize runs the browser flow. open is called with the consent URL (the
// app opens it in the default browser). It returns when Google redirects
// back, ctx ends, or after 5 minutes.
func (c Config) Authorize(ctx context.Context, email string, open func(string) error) (*Token, error) {
	if c.ClientID == "" {
		return nil, errors.New("no Google OAuth client id configured (see docs/gmail-oauth.md), or use an app password")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	redirect := "http://" + ln.Addr().String() + "/callback"
	verifier := random(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := random(16)

	q := url.Values{
		"client_id": {c.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {Scopes}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"state": {state}, "access_type": {"offline"}, "prompt": {"consent"},
	}
	if email != "" {
		q.Set("login_hint", email)
	}
	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		v := r.URL.Query()
		var res result
		switch {
		case v.Get("state") != state:
			res.err = errors.New("sign-in answer did not match the request (state)")
		case v.Get("error") != "":
			res.err = fmt.Errorf("Google said: %s", v.Get("error"))
		default:
			res.code = v.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		msg := "You're signed in. You can close this tab and go back to Laser Email."
		if res.err != nil {
			msg = "Sign-in failed: " + res.err.Error()
		}
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>Laser Email</title><body style="font:16px -apple-system,sans-serif;padding:3em">%s</body>`, html.EscapeString(msg))
		select {
		case done <- res:
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	if err := open(c.authURL() + "?" + q.Encode()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	select {
	case <-ctx.Done():
		return nil, errors.New("sign-in timed out")
	case res := <-done:
		if res.err != nil {
			return nil, res.err
		}
		return c.exchange(ctx, url.Values{
			"grant_type": {"authorization_code"}, "code": {res.code}, "code_verifier": {verifier}, "redirect_uri": {redirect},
		})
	}
}

// Refresh gets a new access token with the refresh token.
func (c Config) Refresh(ctx context.Context, t *Token) (*Token, error) {
	if t.Refresh == "" {
		return nil, errors.New("no refresh token: sign in again")
	}
	nt, err := c.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.Refresh}})
	if err != nil {
		return nil, err
	}
	if nt.Refresh == "" {
		nt.Refresh = t.Refresh
	}
	return nt, nil
}

func (c Config) exchange(ctx context.Context, v url.Values) (*Token, error) {
	v.Set("client_id", c.ClientID)
	if c.ClientSecret != "" {
		v.Set("client_secret", c.ClientSecret)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", c.tokenURL(), strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		ExpiresIn int    `json:"expires_in"`
		IDToken   string `json:"id_token"`
		Error     string `json:"error"`
		Desc      string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("token answer: %w", err)
	}
	if resp.StatusCode != 200 || body.Access == "" {
		return nil, fmt.Errorf("token request failed: %s %s", body.Error, body.Desc)
	}
	return &Token{Access: body.Access, Refresh: body.Refresh, Expiry: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
		Email: emailFromIDToken(body.IDToken)}, nil
}

// emailFromIDToken reads the verified address from an OpenID ID token. It
// came straight from Google's token endpoint over TLS, so (as OpenID Connect
// allows for that case) its signature isn't checked again here.
func emailFromIDToken(t string) string {
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if json.Unmarshal(b, &claims) != nil || !claims.Verified {
		return ""
	}
	return claims.Email
}

// XOAuth2 is the SASL XOAUTH2 mechanism Gmail uses for IMAP and SMTP.
type XOAuth2 struct{ User, Token string }

// Start implements sasl.Client.
func (x XOAuth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.User + "\x01auth=Bearer " + x.Token + "\x01\x01"), nil
}

// Next implements sasl.Client: on failure the server sends a JSON error;
// answering with an empty line ends the exchange so the error surfaces.
func (x XOAuth2) Next(challenge []byte) ([]byte, error) { return []byte{}, nil }
