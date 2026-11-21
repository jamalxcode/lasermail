// Package store is Laser Email's local database: accounts, mailboxes, message
// summaries, raw bodies and a full-text index. It lives in the user's data
// folder (0700) and never leaves the Mac.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jamalxcode/lasermail/internal/msg"
	"github.com/jamalxcode/lasermail/internal/provider"
	_ "modernc.org/sqlite"
)

// Mailbox roles (from IMAP SPECIAL-USE). Unified views group by role.
const (
	Inbox   = "inbox"
	Sent    = "sent"
	Drafts  = "drafts"
	Archive = "archive"
	Trash   = "trash"
	Junk    = "junk"
	All     = "all" // Gmail's All Mail
	Flagged = "flagged"
	Other   = ""
)

// Account is one mail account. Its password or token is in the secret store.
type Account struct {
	ID       int64             `json:"id"`
	Email    string            `json:"email"`
	Name     string            `json:"name"` // the user's display name for From
	Label    string            `json:"label"`
	Auth     provider.Auth     `json:"auth"`
	Settings provider.Settings `json:"settings"`
	Color    string            `json:"color"`
}

// Mailbox is one IMAP folder.
type Mailbox struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"account_id"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	UIDValidity uint32 `json:"-"`
	LastUID     uint32 `json:"-"`
	Unread      int    `json:"unread"`
	Total       int    `json:"total"`
}

// Message is a summary row; the body is fetched on demand.
type Message struct {
	ID             int64         `json:"id"`
	AccountID      int64         `json:"account_id"`
	MailboxID      int64         `json:"mailbox_id"`
	UID            uint32        `json:"uid"`
	MessageID      string        `json:"message_id"`
	InReplyTo      string        `json:"in_reply_to,omitempty"`
	ThreadKey      string        `json:"thread_key"`
	Subject        string        `json:"subject"`
	From           msg.Address   `json:"from"`
	To             []msg.Address `json:"to"`
	Cc             []msg.Address `json:"cc,omitempty"`
	Date           time.Time     `json:"date"`
	Seen           bool          `json:"seen"`
	Flagged        bool          `json:"flagged"`
	Answered       bool          `json:"answered"`
	HasAttachments bool          `json:"has_attachments"`
	Size           int64         `json:"size"`
	Snippet        string        `json:"snippet"`
	HasBody        bool          `json:"has_body"`
}

// Store is the database.
type Store struct{ db *sql.DB }

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS accounts(
  id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, name TEXT NOT NULL DEFAULT '', label TEXT NOT NULL DEFAULT '',
  auth TEXT NOT NULL, settings TEXT NOT NULL, color TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS mailboxes(
  id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  path TEXT NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT '',
  uidvalidity INTEGER NOT NULL DEFAULT 0, last_uid INTEGER NOT NULL DEFAULT 0,
  UNIQUE(account_id, path));
CREATE TABLE IF NOT EXISTS messages(
  id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE, uid INTEGER NOT NULL,
  message_id TEXT NOT NULL DEFAULT '', in_reply_to TEXT NOT NULL DEFAULT '', thread_key TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '', from_name TEXT NOT NULL DEFAULT '', from_email TEXT NOT NULL DEFAULT '',
  to_json TEXT NOT NULL DEFAULT '[]', cc_json TEXT NOT NULL DEFAULT '[]', date INTEGER NOT NULL DEFAULT 0,
  seen INTEGER NOT NULL DEFAULT 0, flagged INTEGER NOT NULL DEFAULT 0, answered INTEGER NOT NULL DEFAULT 0,
  has_attachments INTEGER NOT NULL DEFAULT 0, size INTEGER NOT NULL DEFAULT 0, snippet TEXT NOT NULL DEFAULT '',
  UNIQUE(mailbox_id, uid));
CREATE INDEX IF NOT EXISTS messages_date ON messages(date DESC);
CREATE INDEX IF NOT EXISTS messages_mailbox_date ON messages(mailbox_id, date DESC);
CREATE INDEX IF NOT EXISTS messages_thread ON messages(thread_key);
CREATE TABLE IF NOT EXISTS bodies(message INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE, raw BLOB NOT NULL);
CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(subject, sender, body, content='', contentless_delete=1, tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER IF NOT EXISTS messages_search_delete AFTER DELETE ON messages BEGIN DELETE FROM search WHERE rowid = old.id; END;
CREATE TABLE IF NOT EXISTS senders(email TEXT PRIMARY KEY, load_images INTEGER NOT NULL DEFAULT 0);
`

// Open opens (and creates) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; keeps FTS and triggers simple
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		os.Chmod(p, 0o600)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

var ErrNotFound = errors.New("not found")

// --- accounts ---

func (s *Store) AddAccount(a *Account) error {
	set, _ := json.Marshal(a.Settings)
	res, err := s.db.Exec(`INSERT INTO accounts(email,name,label,auth,settings,color,created) VALUES(?,?,?,?,?,?,?)`,
		strings.ToLower(strings.TrimSpace(a.Email)), a.Name, a.Label, a.Auth, string(set), a.Color, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("%s is already added", a.Email)
		}
		return err
	}
	a.ID, _ = res.LastInsertId()
	return nil
}

func scanAccount(sc interface{ Scan(...any) error }) (*Account, error) {
	var a Account
	var set string
	if err := sc.Scan(&a.ID, &a.Email, &a.Name, &a.Label, &a.Auth, &set, &a.Color); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(set), &a.Settings)
	return &a, nil
}

func (s *Store) Accounts() ([]*Account, error) {
	rows, err := s.db.Query(`SELECT id,email,name,label,auth,settings,color FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Account(id int64) (*Account, error) {
	a, err := scanAccount(s.db.QueryRow(`SELECT id,email,name,label,auth,settings,color FROM accounts WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) UpdateAccount(a *Account) error {
	set, _ := json.Marshal(a.Settings)
	_, err := s.db.Exec(`UPDATE accounts SET name=?,label=?,auth=?,settings=?,color=? WHERE id=?`, a.Name, a.Label, a.Auth, string(set), a.Color, a.ID)
	return err
}

// DeleteAccount removes the account and everything stored for it.
func (s *Store) DeleteAccount(id int64) error {
	_, err := s.db.Exec(`DELETE FROM accounts WHERE id=?`, id)
	return err
}

// --- mailboxes ---

// UpsertMailbox records a folder and returns it.
func (s *Store) UpsertMailbox(accountID int64, path, name, role string) (*Mailbox, error) {
	_, err := s.db.Exec(`INSERT INTO mailboxes(account_id,path,name,role) VALUES(?,?,?,?)
		ON CONFLICT(account_id,path) DO UPDATE SET name=excluded.name, role=excluded.role`, accountID, path, name, role)
	if err != nil {
		return nil, err
	}
	return s.mailboxWhere(`m.account_id=? AND m.path=?`, accountID, path)
}

const mailboxCols = `m.id,m.account_id,m.path,m.name,m.role,m.uidvalidity,m.last_uid,
  (SELECT count(*) FROM messages x WHERE x.mailbox_id=m.id AND x.seen=0),
  (SELECT count(*) FROM messages x WHERE x.mailbox_id=m.id)`

func scanMailbox(sc interface{ Scan(...any) error }) (*Mailbox, error) {
	var m Mailbox
	err := sc.Scan(&m.ID, &m.AccountID, &m.Path, &m.Name, &m.Role, &m.UIDValidity, &m.LastUID, &m.Unread, &m.Total)
	return &m, err
}

func (s *Store) mailboxWhere(where string, args ...any) (*Mailbox, error) {
	m, err := scanMailbox(s.db.QueryRow(`SELECT `+mailboxCols+` FROM mailboxes m WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) Mailbox(id int64) (*Mailbox, error) { return s.mailboxWhere(`m.id=?`, id) }

// MailboxByRole finds an account's folder with a role.
func (s *Store) MailboxByRole(accountID int64, role string) (*Mailbox, error) {
	return s.mailboxWhere(`m.account_id=? AND m.role=? ORDER BY m.id LIMIT 1`, accountID, role)
}

// Mailboxes lists an account's folders (all accounts when accountID is 0).
func (s *Store) Mailboxes(accountID int64) ([]*Mailbox, error) {
	q := `SELECT ` + mailboxCols + ` FROM mailboxes m`
	var args []any
	if accountID != 0 {
		q += ` WHERE m.account_id=?`
		args = append(args, accountID)
	}
	rows, err := s.db.Query(q+` ORDER BY m.account_id, CASE m.role WHEN 'inbox' THEN 0 WHEN 'drafts' THEN 1 WHEN 'sent' THEN 2 WHEN 'junk' THEN 3 WHEN 'trash' THEN 4 WHEN 'archive' THEN 5 WHEN 'all' THEN 6 ELSE 7 END, m.path`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Mailbox
	for rows.Next() {
		m, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMailbox forgets a folder that no longer exists on the server.
func (s *Store) DeleteMailbox(id int64) error {
	_, err := s.db.Exec(`DELETE FROM mailboxes WHERE id=?`, id)
	return err
}

// SetSyncState records the UIDVALIDITY and the highest UID seen.
func (s *Store) SetSyncState(mailboxID int64, uidValidity, lastUID uint32) error {
	_, err := s.db.Exec(`UPDATE mailboxes SET uidvalidity=?, last_uid=? WHERE id=?`, uidValidity, lastUID, mailboxID)
	return err
}

// ResetMailbox drops every message of a folder (its UIDVALIDITY changed).
func (s *Store) ResetMailbox(mailboxID int64) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE mailbox_id=?; UPDATE mailboxes SET last_uid=0 WHERE id=?`, mailboxID, mailboxID)
	return err
}

// --- messages ---

// ThreadKey groups a message with its replies: the first reference (the
// thread's root), else what it replies to, else its own id.
func ThreadKey(messageID, inReplyTo string, refs []string) string {
	switch {
	case len(refs) > 0:
		return refs[0]
	case inReplyTo != "":
		return inReplyTo
	}
	return messageID
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// PutMessage inserts a summary; an existing (mailbox, uid) only gets its
// flags updated. It returns the row id.
func (s *Store) PutMessage(m *Message) (int64, error) {
	to, _ := json.Marshal(nonNil(m.To))
	cc, _ := json.Marshal(nonNil(m.Cc))
	if m.ThreadKey == "" {
		m.ThreadKey = m.MessageID
	}
	err := s.db.QueryRow(`INSERT INTO messages(account_id,mailbox_id,uid,message_id,in_reply_to,thread_key,subject,from_name,from_email,
		to_json,cc_json,date,seen,flagged,answered,has_attachments,size,snippet) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(mailbox_id,uid) DO UPDATE SET seen=excluded.seen, flagged=excluded.flagged, answered=excluded.answered
		RETURNING id`,
		m.AccountID, m.MailboxID, m.UID, m.MessageID, m.InReplyTo, m.ThreadKey, m.Subject, m.From.Name, strings.ToLower(m.From.Email),
		string(to), string(cc), m.Date.Unix(), boolInt(m.Seen), boolInt(m.Flagged), boolInt(m.Answered),
		boolInt(m.HasAttachments), m.Size, m.Snippet).Scan(&m.ID)
	if err != nil {
		return 0, err
	}
	// Index what we know so far; SetBody adds the text later.
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO search(rowid,subject,sender,body) VALUES(?,?,?,?)`,
		m.ID, m.Subject, m.From.Name+" "+m.From.Email, m.Snippet); err != nil {
		return 0, err
	}
	return m.ID, nil
}

func nonNil(a []msg.Address) []msg.Address {
	if a == nil {
		return []msg.Address{}
	}
	return a
}

// SetFlags updates the flags of a message by UID.
func (s *Store) SetFlags(mailboxID int64, uid uint32, seen, flagged, answered bool) error {
	_, err := s.db.Exec(`UPDATE messages SET seen=?, flagged=?, answered=? WHERE mailbox_id=? AND uid=?`,
		boolInt(seen), boolInt(flagged), boolInt(answered), mailboxID, uid)
	return err
}

// SetLocalFlag changes seen or flagged on our copy (after the server agreed).
func (s *Store) SetLocalFlag(id int64, flag string, on bool) error {
	if flag != "seen" && flag != "flagged" && flag != "answered" {
		return fmt.Errorf("unknown flag %q", flag)
	}
	_, err := s.db.Exec(`UPDATE messages SET `+flag+`=? WHERE id=?`, boolInt(on), id)
	return err
}

// UIDs lists the UIDs we hold for a folder.
func (s *Store) UIDs(mailboxID int64) (map[uint32]int64, error) {
	rows, err := s.db.Query(`SELECT uid,id FROM messages WHERE mailbox_id=?`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint32]int64{}
	for rows.Next() {
		var u uint32
		var id int64
		rows.Scan(&u, &id)
		out[u] = id
	}
	return out, rows.Err()
}

// DeleteMessage removes one message row (it was expunged or moved).
func (s *Store) DeleteMessage(id int64) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE id=?`, id)
	return err
}

// SetBody stores the raw message and indexes its text.
func (s *Store) SetBody(id int64, raw []byte, text, snippet string, hasAttachments bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR REPLACE INTO bodies(message,raw) VALUES(?,?)`, id, raw); err != nil {
		return err
	}
	var subject, fromName, fromEmail string
	if err := tx.QueryRow(`SELECT subject,from_name,from_email FROM messages WHERE id=?`, id).Scan(&subject, &fromName, &fromEmail); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE messages SET snippet=?, has_attachments=? WHERE id=?`, snippet, boolInt(hasAttachments), id); err != nil {
		return err
	}
	if len(text) > 256<<10 {
		text = text[:256<<10]
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO search(rowid,subject,sender,body) VALUES(?,?,?,?)`, id, subject, fromName+" "+fromEmail, text); err != nil {
		return err
	}
	return tx.Commit()
}

// Body returns the raw message, or ErrNotFound when it isn't downloaded.
func (s *Store) Body(id int64) ([]byte, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT raw FROM bodies WHERE message=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return raw, err
}

const messageCols = `m.id,m.account_id,m.mailbox_id,m.uid,m.message_id,m.in_reply_to,m.thread_key,m.subject,m.from_name,m.from_email,
  m.to_json,m.cc_json,m.date,m.seen,m.flagged,m.answered,m.has_attachments,m.size,m.snippet,
  EXISTS(SELECT 1 FROM bodies b WHERE b.message=m.id)`

func scanMessage(sc interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var to, cc string
	var date int64
	err := sc.Scan(&m.ID, &m.AccountID, &m.MailboxID, &m.UID, &m.MessageID, &m.InReplyTo, &m.ThreadKey, &m.Subject,
		&m.From.Name, &m.From.Email, &to, &cc, &date, &m.Seen, &m.Flagged, &m.Answered, &m.HasAttachments, &m.Size, &m.Snippet, &m.HasBody)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(to), &m.To)
	json.Unmarshal([]byte(cc), &m.Cc)
	m.Date = time.Unix(date, 0).UTC()
	return &m, nil
}

func (s *Store) Message(id int64) (*Message, error) {
	m, err := scanMessage(s.db.QueryRow(`SELECT `+messageCols+` FROM messages m WHERE m.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// Query selects messages for a list.
type Query struct {
	AccountID int64     `json:"account_id"` // 0 = all accounts
	MailboxID int64     `json:"mailbox_id"` // a specific folder…
	Role      string    `json:"role"`       // …or every folder with this role (the unified view)
	Unread    bool      `json:"unread"`
	Flagged   bool      `json:"flagged"` // flagged in any folder (Role and MailboxID ignored)
	Text      string    `json:"text"`    // full-text search
	Before    time.Time `json:"before"`  // paging: older than this
	Limit     int       `json:"limit"`
}

// List runs a query, newest first.
func (s *Store) List(q Query) ([]*Message, error) {
	var where []string
	var args []any
	from := `messages m`
	if q.Text != "" {
		fq := ftsQuery(q.Text)
		if fq == "" {
			return nil, nil
		}
		from = `search JOIN messages m ON m.id = search.rowid`
		where = append(where, `search MATCH ?`)
		args = append(args, fq)
	}
	if q.AccountID != 0 {
		where = append(where, `m.account_id=?`)
		args = append(args, q.AccountID)
	}
	switch {
	case q.Flagged:
		// Same rule as the badge: not in Trash, and not Gmail's All Mail copy.
		where = append(where, `m.flagged=1`, `m.mailbox_id NOT IN (SELECT id FROM mailboxes WHERE role IN ('trash','all'))`)
	case q.MailboxID != 0:
		where = append(where, `m.mailbox_id=?`)
		args = append(args, q.MailboxID)
	case q.Role != "":
		where = append(where, `m.mailbox_id IN (SELECT id FROM mailboxes WHERE role=?)`)
		args = append(args, q.Role)
	case q.Text != "":
		// Searching everywhere: skip Trash, Junk and Gmail's All Mail copies.
		where = append(where, `m.mailbox_id NOT IN (SELECT id FROM mailboxes WHERE role IN ('trash','junk','all'))`)
	}
	if q.Unread {
		where = append(where, `m.seen=0`)
	}
	if !q.Before.IsZero() {
		where = append(where, `m.date<?`)
		args = append(args, q.Before.Unix())
	}
	limit := q.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	sqlq := `SELECT ` + messageCols + ` FROM ` + from
	if len(where) > 0 {
		sqlq += ` WHERE ` + strings.Join(where, ` AND `)
	}
	sqlq += fmt.Sprintf(` ORDER BY m.date DESC, m.id DESC LIMIT %d`, limit)
	rows, err := s.db.Query(sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Thread lists the messages sharing a thread key, oldest first.
func (s *Store) Thread(key string) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages m WHERE m.thread_key=? OR m.message_id=?
		ORDER BY m.date, m.id`, key, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	seen := map[string]bool{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		if m.MessageID != "" && seen[m.MessageID] { // same message in two folders (Gmail labels)
			continue
		}
		seen[m.MessageID] = true
		out = append(out, m)
	}
	return out, rows.Err()
}

// ftsQuery turns user words into an FTS5 query: every word must match, as a
// prefix. Quotes and operators are stripped so input can't break the syntax.
func ftsQuery(text string) string {
	var terms []string
	for _, w := range strings.Fields(text) {
		w = strings.Map(func(r rune) rune {
			if r == '"' || r == '*' || r == '(' || r == ')' || r == ':' || r == '^' || r == '-' || r == '+' {
				return -1
			}
			return r
		}, w)
		if w != "" {
			terms = append(terms, `"`+w+`"*`)
		}
	}
	return strings.Join(terms, " ")
}

// Counts are the sidebar badges: unread per role across all accounts.
func (s *Store) Counts() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT b.role, count(*) FROM messages m JOIN mailboxes b ON b.id=m.mailbox_id
		WHERE m.seen=0 AND b.role!='' GROUP BY b.role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var r string
		var n int
		rows.Scan(&r, &n)
		out[r] = n
	}
	var f int
	s.db.QueryRow(`SELECT count(*) FROM messages m JOIN mailboxes b ON b.id=m.mailbox_id WHERE m.flagged=1 AND b.role NOT IN ('all','trash')`).Scan(&f)
	out[Flagged] = f
	return out, rows.Err()
}

// --- sender preferences ---

func (s *Store) LoadImages(email string) bool {
	var v bool
	s.db.QueryRow(`SELECT load_images FROM senders WHERE email=?`, strings.ToLower(email)).Scan(&v)
	return v
}

func (s *Store) SetLoadImages(email string, on bool) error {
	_, err := s.db.Exec(`INSERT INTO senders(email,load_images) VALUES(?,?) ON CONFLICT(email) DO UPDATE SET load_images=excluded.load_images`,
		strings.ToLower(email), boolInt(on))
	return err
}
