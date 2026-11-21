// Package engine is Laser Email's core: it owns the store, the accounts and
// their secrets, one sync worker per account (IDLE push for the inbox), and
// every action the app can take. The daemon exposes it over the socket; the
// tests drive it directly.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jamalxcode/laser-email/internal/imapsync"
	"github.com/jamalxcode/laser-email/internal/oauth"
	"github.com/jamalxcode/laser-email/internal/provider"
	"github.com/jamalxcode/laser-email/internal/secret"
	"github.com/jamalxcode/laser-email/internal/store"
)

// Config for New.
type Config struct {
	DataDir string
	Secrets secret.Store       // nil: the platform default (Keychain on macOS)
	OAuth   oauth.Config       // Google client for "Sign in with Google"
	Sync    *imapsync.Options  // nil: imapsync.DefaultOptions
	OpenURL func(string) error // opens the browser for OAuth
	// FullSyncEvery is how often every folder is re-checked (IDLE covers the
	// inbox in between). Default 10 minutes.
	FullSyncEvery time.Duration
	Logf          func(format string, args ...any)
	// Offline: start no sync workers (demo data, screenshots).
	Offline bool
}

// Engine is the running core.
type Engine struct {
	cfg     Config
	st      *store.Store
	secrets secret.Store
	syncOpt imapsync.Options

	mu      sync.Mutex
	workers map[int64]*worker
	status  map[int64]*AccountStatus
	subs    map[chan Event]struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// Event tells the app something changed.
type Event struct {
	Type      string `json:"type"` // "sync", "new_mail", "status", "accounts"
	AccountID int64  `json:"account_id,omitempty"`
	MailboxID int64  `json:"mailbox_id,omitempty"`
	Count     int    `json:"count,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AccountStatus is shown under each account in the sidebar.
type AccountStatus struct {
	State    string    `json:"state"` // "connecting", "syncing", "idle", "error", "offline"
	Error    string    `json:"error,omitempty"`
	LastSync time.Time `json:"last_sync"`
}

// New opens the store and starts a worker per account.
func New(cfg Config) (*Engine, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("no data folder")
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "laser.db"))
	if err != nil {
		return nil, err
	}
	if cfg.Secrets == nil {
		cfg.Secrets = secret.Default(cfg.DataDir)
	}
	if cfg.FullSyncEvery == 0 {
		cfg.FullSyncEvery = 10 * time.Minute
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	e := &Engine{cfg: cfg, st: st, secrets: cfg.Secrets, syncOpt: imapsync.DefaultOptions,
		workers: map[int64]*worker{}, status: map[int64]*AccountStatus{}, subs: map[chan Event]struct{}{}}
	if cfg.Sync != nil {
		e.syncOpt = *cfg.Sync
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	accts, err := st.Accounts()
	if err != nil {
		st.Close()
		return nil, err
	}
	for _, a := range accts {
		e.startWorker(a)
	}
	return e, nil
}

// Close stops the workers and closes the store.
func (e *Engine) Close() error {
	e.cancel()
	e.mu.Lock()
	for _, w := range e.workers {
		w.closeActionConn()
	}
	e.mu.Unlock()
	e.wg.Wait()
	return e.st.Close()
}

// Store gives tests and the CLI read access.
func (e *Engine) Store() *store.Store { return e.st }

// Subscribe returns a channel of events; call the func to stop.
func (e *Engine) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		if _, ok := e.subs[ch]; ok {
			delete(e.subs, ch)
			close(ch)
		}
		e.mu.Unlock()
	}
}

func (e *Engine) emit(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- ev:
		default: // a slow listener misses events rather than stalling sync
		}
	}
}

func (e *Engine) setStatus(id int64, state string, err error) {
	e.mu.Lock()
	s := e.status[id]
	if s == nil {
		s = &AccountStatus{}
		e.status[id] = s
	}
	s.State = state
	s.Error = ""
	if err != nil {
		s.Error = err.Error()
	}
	if state == "idle" {
		s.LastSync = time.Now()
	}
	e.mu.Unlock()
	ev := Event{Type: "status", AccountID: id}
	if err != nil {
		ev.Error = err.Error()
	}
	e.emit(ev)
}

// Status of every account.
func (e *Engine) Status() map[int64]AccountStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[int64]AccountStatus{}
	for id, s := range e.status {
		out[id] = *s
	}
	return out
}

// --- accounts ---

// NewAccount is what the "Add Account" sheet sends.
type NewAccount struct {
	Email    string        `json:"email"`
	Name     string        `json:"name"`
	Label    string        `json:"label"`
	Kind     provider.Kind `json:"kind"`     // gmail, icloud, imap; empty = detect
	Password string        `json:"password"` // app password
	OAuth    bool          `json:"oauth"`    // Gmail: sign in with Google instead
	// Settings override the preset (required for kind "imap").
	Settings *provider.Settings `json:"settings,omitempty"`
}

func passwordKey(id int64) string { return fmt.Sprintf("account-%d-password", id) }
func tokenKey(id int64) string    { return fmt.Sprintf("account-%d-oauth", id) }

// AddAccount checks the login, then saves the account and starts syncing it.
func (e *Engine) AddAccount(ctx context.Context, n NewAccount) (*store.Account, error) {
	n.Email = strings.TrimSpace(n.Email)
	if !strings.Contains(n.Email, "@") {
		return nil, errors.New("enter a full email address")
	}
	if n.Kind == "" {
		n.Kind = provider.Detect(n.Email)
	}
	var set provider.Settings
	if n.Settings != nil {
		set = *n.Settings
		set.Kind = n.Kind
	} else if p, ok := provider.Preset(n.Kind); ok {
		set = p
	} else {
		return nil, errors.New("enter the IMAP and SMTP server settings for this account")
	}
	a := &store.Account{Email: n.Email, Name: n.Name, Label: n.Label, Settings: set, Auth: provider.Password}
	var tok *oauth.Token
	if n.OAuth {
		if n.Kind != provider.Gmail {
			return nil, errors.New("Sign in with Google is only for Gmail accounts")
		}
		open := e.cfg.OpenURL
		if open == nil {
			return nil, errors.New("no browser available for Google sign-in")
		}
		var err error
		if tok, err = e.cfg.OAuth.Authorize(ctx, n.Email, open); err != nil {
			return nil, err
		}
		a.Auth = provider.OAuth
	} else {
		n.Password = strings.ReplaceAll(n.Password, " ", "") // Google shows app passwords in groups of four
		if n.Password == "" {
			return nil, errors.New("enter the app password")
		}
	}
	cred := imapsync.Credentials{User: provider.LoginName(n.Kind, n.Email), Password: n.Password}
	if tok != nil {
		cred = imapsync.Credentials{SASL: oauth.XOAuth2{User: n.Email, Token: tok.Access}}
	}
	c, err := imapsync.Dial(ctx, set.IMAP, cred)
	if err != nil {
		return nil, err
	}
	c.Close()
	if a.Label == "" {
		a.Label = map[provider.Kind]string{provider.Gmail: "Gmail", provider.ICloud: "iCloud"}[n.Kind]
		if a.Label == "" {
			a.Label = set.IMAP.Host
		}
	}
	if err := e.st.AddAccount(a); err != nil {
		return nil, err
	}
	if tok != nil {
		b, _ := json.Marshal(tok)
		err = e.secrets.Set(tokenKey(a.ID), string(b))
	} else {
		err = e.secrets.Set(passwordKey(a.ID), n.Password)
	}
	if err != nil {
		e.st.DeleteAccount(a.ID)
		return nil, fmt.Errorf("save to Keychain: %w", err)
	}
	e.startWorker(a)
	e.emit(Event{Type: "accounts"})
	return a, nil
}

// RemoveAccount stops syncing, deletes its secrets and every local copy.
func (e *Engine) RemoveAccount(id int64) error {
	e.mu.Lock()
	w := e.workers[id]
	delete(e.workers, id)
	delete(e.status, id)
	e.mu.Unlock()
	if w != nil {
		w.stop()
	}
	e.secrets.Delete(passwordKey(id))
	e.secrets.Delete(tokenKey(id))
	if err := e.st.DeleteAccount(id); err != nil {
		return err
	}
	e.emit(Event{Type: "accounts"})
	return nil
}

// UpdatePassword replaces an account's app password (after it was revoked).
func (e *Engine) UpdatePassword(ctx context.Context, id int64, password string) error {
	a, err := e.st.Account(id)
	if err != nil {
		return err
	}
	password = strings.ReplaceAll(password, " ", "")
	c, err := imapsync.Dial(ctx, a.Settings.IMAP, imapsync.Credentials{User: provider.LoginName(a.Settings.Kind, a.Email), Password: password})
	if err != nil {
		return err
	}
	c.Close()
	if err := e.secrets.Set(passwordKey(id), password); err != nil {
		return err
	}
	e.SyncNow(id)
	return nil
}

// credentials loads (and refreshes) what logs an account in.
func (e *Engine) credentials(ctx context.Context, a *store.Account) (imapsync.Credentials, string, error) {
	user := provider.LoginName(a.Settings.Kind, a.Email)
	if a.Auth != provider.OAuth {
		pw, err := e.secrets.Get(passwordKey(a.ID))
		if err != nil {
			return imapsync.Credentials{}, "", fmt.Errorf("no password in Keychain for %s: %w", a.Email, err)
		}
		return imapsync.Credentials{User: user, Password: pw}, "", nil
	}
	raw, err := e.secrets.Get(tokenKey(a.ID))
	if err != nil {
		return imapsync.Credentials{}, "", fmt.Errorf("not signed in to Google for %s: %w", a.Email, err)
	}
	var tok oauth.Token
	json.Unmarshal([]byte(raw), &tok)
	if !tok.Valid() {
		nt, err := e.cfg.OAuth.Refresh(ctx, &tok)
		if err != nil {
			return imapsync.Credentials{}, "", fmt.Errorf("Google sign-in expired for %s: %w", a.Email, err)
		}
		b, _ := json.Marshal(nt)
		e.secrets.Set(tokenKey(a.ID), string(b))
		tok = *nt
	}
	return imapsync.Credentials{User: user, SASL: oauth.XOAuth2{User: a.Email, Token: tok.Access}}, tok.Access, nil
}

func (e *Engine) dial(ctx context.Context, a *store.Account) (*imapsync.Conn, error) {
	cred, _, err := e.credentials(ctx, a)
	if err != nil {
		return nil, err
	}
	return imapsync.Dial(ctx, a.Settings.IMAP, cred)
}

// Accounts lists the accounts.
func (e *Engine) Accounts() ([]*store.Account, error) { return e.st.Accounts() }
