package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/jamalxcode/laser-email/internal/imapsync"
	"github.com/jamalxcode/laser-email/internal/msg"
	"github.com/jamalxcode/laser-email/internal/privacy"
	"github.com/jamalxcode/laser-email/internal/send"
	"github.com/jamalxcode/laser-email/internal/store"
)

// Mailboxes of one account (0 = all).
func (e *Engine) Mailboxes(accountID int64) ([]*store.Mailbox, error) {
	return e.st.Mailboxes(accountID)
}

// List messages.
func (e *Engine) List(q store.Query) ([]*store.Message, error) { return e.st.List(q) }

// Counts of unread mail per role, for the sidebar.
func (e *Engine) Counts() (map[string]int, error) { return e.st.Counts() }

// View is an opened message.
type View struct {
	*store.Message
	Mailbox     string           `json:"mailbox"`
	Account     string           `json:"account"`
	ReplyTo     []msg.Address    `json:"reply_to,omitempty"`
	HTML        string           `json:"html"` // sanitized, in a document with a CSP
	Privacy     privacy.Report   `json:"privacy"`
	Remote      bool             `json:"remote_images"` // remote images are loaded
	Attachments []msg.Attachment `json:"attachments"`
	Unsubscribe string           `json:"list_unsubscribe,omitempty"`
	ReadReceipt bool             `json:"read_receipt_requested"`
	Thread      []*store.Message `json:"thread,omitempty"`
}

// OpenOptions for Open.
type OpenOptions struct {
	LoadImages bool `json:"load_images"` // this once
	MarkSeen   bool `json:"mark_seen"`
}

// Open fetches (if needed), sanitizes and returns a message.
func (e *Engine) Open(ctx context.Context, id int64, o OpenOptions) (*View, error) {
	m, err := e.st.Message(id)
	if err != nil {
		return nil, err
	}
	mb, err := e.st.Mailbox(m.MailboxID)
	if err != nil {
		return nil, err
	}
	raw, err := e.st.Body(id)
	if errors.Is(err, store.ErrNotFound) {
		if err := e.withConn(ctx, m.AccountID, func(c *imapsync.Conn) error { return c.FetchBody(e.st, mb, m.UID) }); err != nil {
			return nil, err
		}
		raw, err = e.st.Body(id)
	}
	if err != nil {
		return nil, err
	}
	p, err := msg.Parse(raw)
	if err != nil {
		return nil, err
	}
	v := &View{Message: m, Mailbox: mb.Name, ReplyTo: p.ReplyTo, Attachments: p.Attachments, Unsubscribe: p.ListUnsubscribe, ReadReceipt: p.WantsReadReceipt}
	if a, err := e.st.Account(m.AccountID); err == nil {
		v.Account = a.Email
	}
	v.Remote = o.LoadImages || e.st.LoadImages(m.From.Email)
	body := p.HTML
	if strings.TrimSpace(body) == "" {
		body = msg.TextToHTML(p.Text)
	}
	r := privacy.Sanitize(body, privacy.Options{AllowRemoteImages: v.Remote})
	v.HTML, v.Privacy = r.HTML, r.Report
	if m.ThreadKey != "" {
		if t, err := e.st.Thread(m.ThreadKey); err == nil && len(t) > 1 {
			v.Thread = t
		}
	}
	if o.MarkSeen && !m.Seen {
		if err := e.SetFlag(ctx, id, "seen", true); err == nil {
			v.Seen = true
		}
	}
	return v, nil
}

// SetLoadImages remembers "always load images from this sender".
func (e *Engine) SetLoadImages(email string, on bool) error { return e.st.SetLoadImages(email, on) }

// SetFlag marks read/unread or flagged on the server, then locally.
func (e *Engine) SetFlag(ctx context.Context, id int64, flag string, on bool) error {
	m, mb, err := e.msgBox(id)
	if err != nil {
		return err
	}
	if err := e.withConn(ctx, m.AccountID, func(c *imapsync.Conn) error { return c.SetFlag(mb, m.UID, flag, on) }); err != nil {
		return err
	}
	return e.st.SetLocalFlag(id, flag, on)
}

func (e *Engine) msgBox(id int64) (*store.Message, *store.Mailbox, error) {
	m, err := e.st.Message(id)
	if err != nil {
		return nil, nil, err
	}
	mb, err := e.st.Mailbox(m.MailboxID)
	return m, mb, err
}

// MoveTo moves a message to a folder of the same account.
func (e *Engine) MoveTo(ctx context.Context, id, mailboxID int64) error {
	m, from, err := e.msgBox(id)
	if err != nil {
		return err
	}
	to, err := e.st.Mailbox(mailboxID)
	if err != nil {
		return err
	}
	if to.AccountID != m.AccountID {
		return errors.New("messages can only move within their account")
	}
	if to.ID == from.ID {
		return nil
	}
	if err := e.withConn(ctx, m.AccountID, func(c *imapsync.Conn) error { return c.Move(from, m.UID, to.Path) }); err != nil {
		return err
	}
	if err := e.st.DeleteMessage(id); err != nil {
		return err
	}
	e.SyncNow(m.AccountID) // picks the message up in its new folder
	return nil
}

// MoveToRole moves to the account's Archive, Trash or Junk folder.
func (e *Engine) MoveToRole(ctx context.Context, id int64, role string) error {
	m, err := e.st.Message(id)
	if err != nil {
		return err
	}
	to, err := e.st.MailboxByRole(m.AccountID, role)
	if errors.Is(err, store.ErrNotFound) && role == store.Archive {
		to, err = e.st.MailboxByRole(m.AccountID, store.All) // Gmail: archive = leave the inbox
	}
	if err != nil {
		return fmt.Errorf("this account has no %s folder", role)
	}
	return e.MoveTo(ctx, id, to.ID)
}

// Archive moves a message out of the inbox.
func (e *Engine) Archive(ctx context.Context, id int64) error {
	return e.MoveToRole(ctx, id, store.Archive)
}

// Delete moves a message to Trash; inside Trash it deletes it for good.
func (e *Engine) Delete(ctx context.Context, id int64) error {
	m, mb, err := e.msgBox(id)
	if err != nil {
		return err
	}
	if mb.Role == store.Trash {
		if err := e.withConn(ctx, m.AccountID, func(c *imapsync.Conn) error { return c.Delete(mb, m.UID) }); err != nil {
			return err
		}
		return e.st.DeleteMessage(id)
	}
	return e.MoveToRole(ctx, id, store.Trash)
}

// SaveAttachment writes attachment index of message id into dir (it never
// overwrites: "name 2.pdf" etc.) and returns the path.
func (e *Engine) SaveAttachment(ctx context.Context, id int64, index int, dir string) (string, error) {
	if _, err := e.Open(ctx, id, OpenOptions{}); err != nil { // makes sure the body is here
		return "", err
	}
	raw, err := e.st.Body(id)
	if err != nil {
		return "", err
	}
	a, data, err := msg.AttachmentData(raw, index)
	if err != nil {
		return "", err
	}
	name := filepath.Base(strings.ReplaceAll(a.Name, "\x00", ""))
	if name == "." || name == "/" || name == ".." || strings.HasPrefix(name, ".") {
		name = "attachment" + name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			name = fmt.Sprintf("%s %d%s", stem, i+1, ext)
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return p, err
	}
}

// --- composing ---

// Send delivers a draft from an account and files a copy in Sent (unless
// the server does that itself, like Gmail).
func (e *Engine) Send(ctx context.Context, accountID int64, d *msg.Draft) error {
	a, err := e.st.Account(accountID)
	if err != nil {
		return err
	}
	if len(d.Recipients()) == 0 {
		return errors.New("add at least one recipient")
	}
	d.From = msg.Address{Name: a.Name, Email: a.Email}
	raw, _, err := msg.Build(d, time.Now())
	if err != nil {
		return err
	}
	cred, bearer, err := e.credentials(ctx, a)
	if err != nil {
		return err
	}
	sc := send.Credentials{User: cred.User, Password: cred.Password, OAuthBearer: bearer}
	if err := send.Send(ctx, a.Settings.SMTP, sc, a.Email, d.Recipients(), raw); err != nil {
		return err
	}
	if d.InReplyTo != "" {
		e.markAnswered(ctx, accountID, d.InReplyTo)
	}
	if !a.Settings.SavesSent {
		if sent, err := e.st.MailboxByRole(accountID, store.Sent); err == nil {
			err := e.withConn(ctx, accountID, func(c *imapsync.Conn) error { return c.Append(sent.Path, raw, imap.FlagSeen) })
			if err != nil {
				e.cfg.Logf("account %d: sent, but could not save a copy in Sent: %v", accountID, err)
			}
		}
	}
	e.SyncNow(accountID)
	return nil
}

func (e *Engine) markAnswered(ctx context.Context, accountID int64, messageID string) {
	l, _ := e.st.Thread(messageID)
	for _, m := range l {
		if m.MessageID == messageID && m.AccountID == accountID && !m.Answered {
			e.SetFlag(ctx, m.ID, "answered", true)
		}
	}
}

// Reply builds a reply (all = Reply All) to message id.
func (e *Engine) Reply(ctx context.Context, id int64, all bool) (*msg.Draft, int64, error) {
	v, err := e.Open(ctx, id, OpenOptions{})
	if err != nil {
		return nil, 0, err
	}
	raw, _ := e.st.Body(id)
	p, err := msg.Parse(raw)
	if err != nil {
		return nil, 0, err
	}
	a, _ := e.st.Account(v.AccountID)
	to := p.ReplyTo
	if len(to) == 0 {
		to = p.From
	}
	d := &msg.Draft{Subject: msg.ReplySubject(p.Subject), To: to, InReplyTo: p.MessageID}
	d.References = append(append([]string{}, p.References...), p.MessageID)
	if all {
		seen := map[string]bool{strings.ToLower(a.Email): true}
		for _, x := range to {
			seen[strings.ToLower(x.Email)] = true
		}
		for _, x := range append(append([]msg.Address{}, p.To...), p.Cc...) {
			if k := strings.ToLower(x.Email); !seen[k] {
				seen[k] = true
				d.Cc = append(d.Cc, x)
			}
		}
	}
	d.Text = "\n\n" + quoteHeader(p) + "\n" + quote(p.PlainText())
	return d, v.AccountID, nil
}

// Forward builds a forward of message id (attachments included).
func (e *Engine) Forward(ctx context.Context, id int64) (*msg.Draft, int64, error) {
	v, err := e.Open(ctx, id, OpenOptions{})
	if err != nil {
		return nil, 0, err
	}
	raw, _ := e.st.Body(id)
	p, err := msg.Parse(raw)
	if err != nil {
		return nil, 0, err
	}
	d := &msg.Draft{Subject: msg.ForwardSubject(p.Subject)}
	var from string
	if len(p.From) > 0 {
		from = p.From[0].String()
	}
	d.Text = fmt.Sprintf("\n\nBegin forwarded message:\n\nFrom: %s\nSubject: %s\nDate: %s\n\n%s", from, p.Subject, p.Date.Format(time.RFC1123Z), p.PlainText())
	for _, at := range p.Attachments {
		if _, data, err := msg.AttachmentData(raw, at.Index); err == nil {
			d.Files = append(d.Files, msg.File{Name: at.Name, Type: at.Type, Data: data})
		}
	}
	return d, v.AccountID, nil
}

func quoteHeader(p *msg.Parsed) string {
	who := "someone"
	if len(p.From) > 0 {
		who = p.From[0].String()
	}
	return fmt.Sprintf("On %s, %s wrote:", p.Date.Local().Format("Jan 2, 2006, at 3:04 PM"), who)
}

func quote(s string) string {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, ">") {
			lines[i] = ">" + l
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

// InlinePart returns an inline image (cid:) of a message, for the reader.
func (e *Engine) InlinePart(ctx context.Context, id int64, contentID string) (msg.Attachment, []byte, error) {
	raw, err := e.st.Body(id)
	if err != nil {
		return msg.Attachment{}, nil, err
	}
	p, err := msg.Parse(raw)
	if err != nil {
		return msg.Attachment{}, nil, err
	}
	for _, a := range p.Attachments {
		if a.ContentID != "" && a.ContentID == strings.Trim(contentID, "<>") {
			return msg.AttachmentData(raw, a.Index)
		}
	}
	return msg.Attachment{}, nil, store.ErrNotFound
}
