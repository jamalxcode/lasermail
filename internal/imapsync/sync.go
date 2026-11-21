package imapsync

import (
	"fmt"
	"sort"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/jamalxcode/lasermail/internal/msg"
	"github.com/jamalxcode/lasermail/internal/store"
)

// Options tune how much is downloaded.
type Options struct {
	// Window is how many of the newest messages a folder starts with.
	Window int
	// Prefetch is how many of the newest messages get their full body
	// downloaded during sync (for search and previews); others on open.
	Prefetch int
	// MaxPrefetchSize skips bodies larger than this during prefetch.
	MaxPrefetchSize int64
}

// DefaultOptions keeps a first sync quick on big accounts.
var DefaultOptions = Options{Window: 2000, Prefetch: 300, MaxPrefetchSize: 2 << 20}

// roleOf maps a folder's attributes (and, failing that, its name) to a role.
func roleOf(l *imap.ListData) string {
	if strings.EqualFold(l.Mailbox, "INBOX") {
		return store.Inbox
	}
	for _, a := range l.Attrs {
		switch a {
		case imap.MailboxAttrSent:
			return store.Sent
		case imap.MailboxAttrDrafts:
			return store.Drafts
		case imap.MailboxAttrTrash:
			return store.Trash
		case imap.MailboxAttrJunk:
			return store.Junk
		case imap.MailboxAttrArchive:
			return store.Archive
		case imap.MailboxAttrAll:
			return store.All
		}
	}
	// Servers without SPECIAL-USE: well-known names (iCloud's included).
	name := strings.ToLower(leaf(l.Mailbox, l.Delim))
	switch name {
	case "sent", "sent messages", "sent items", "sent mail":
		return store.Sent
	case "drafts":
		return store.Drafts
	case "trash", "deleted messages", "deleted items", "bin":
		return store.Trash
	case "junk", "spam", "junk e-mail":
		return store.Junk
	case "archive", "archives":
		return store.Archive
	}
	return store.Other
}

func leaf(path string, delim rune) string {
	if delim != 0 {
		if i := strings.LastIndex(path, string(delim)); i >= 0 {
			return path[i+len(string(delim)):]
		}
	}
	return path
}

var roleNames = map[string]string{
	store.Inbox: "Inbox", store.Sent: "Sent", store.Drafts: "Drafts", store.Trash: "Trash",
	store.Junk: "Junk", store.Archive: "Archive", store.All: "All Mail",
}

// SyncFolders records the account's folders and forgets ones that are gone.
func (c *Conn) SyncFolders(st *store.Store, accountID int64) ([]*store.Mailbox, error) {
	opts := &imap.ListOptions{}
	if c.c.Caps().Has(imap.CapSpecialUse) {
		opts.ReturnSpecialUse = true
	}
	list, err := c.c.List("", "*", opts).Collect()
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	keep := map[string]bool{}
	for _, l := range list {
		if hasAttr(l, imap.MailboxAttrNoSelect) || hasAttr(l, imap.MailboxAttrNonExistent) {
			continue
		}
		role := roleOf(l)
		name := leaf(l.Mailbox, l.Delim)
		if n, ok := roleNames[role]; ok {
			name = n
		}
		if _, err := st.UpsertMailbox(accountID, l.Mailbox, name, role); err != nil {
			return nil, err
		}
		keep[l.Mailbox] = true
	}
	have, err := st.Mailboxes(accountID)
	if err != nil {
		return nil, err
	}
	var out []*store.Mailbox
	for _, m := range have {
		if !keep[m.Path] {
			st.DeleteMailbox(m.ID)
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func hasAttr(l *imap.ListData, a imap.MailboxAttr) bool {
	for _, x := range l.Attrs {
		if x == a {
			return true
		}
	}
	return false
}

// Result of a folder sync.
type Result struct {
	New     int `json:"new"`
	Changed int `json:"changed"`
	Removed int `json:"removed"`
}

var refsSection = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}, Peek: true}

// Select opens a folder for reading and writing.
func (c *Conn) Select(path string) (*imap.SelectData, error) {
	return c.c.Select(path, nil).Wait()
}

// SyncMailbox brings one folder up to date.
func (c *Conn) SyncMailbox(st *store.Store, mb *store.Mailbox, o Options) (Result, error) {
	var res Result
	sel, err := c.Select(mb.Path)
	if err != nil {
		return res, fmt.Errorf("open %s: %w", mb.Path, err)
	}
	if mb.UIDValidity != 0 && sel.UIDValidity != mb.UIDValidity {
		if err := st.ResetMailbox(mb.ID); err != nil {
			return res, err
		}
		mb.LastUID = 0
	}
	known, err := st.UIDs(mb.ID)
	if err != nil {
		return res, err
	}

	// 1. Flags and deletions for what we already hold.
	if len(known) > 0 && sel.NumMessages > 0 {
		lo := imap.UID(^uint32(0))
		for u := range known {
			lo = min(lo, imap.UID(u))
		}
		var set imap.UIDSet
		set.AddRange(lo, imap.UID(mb.LastUID))
		msgs, err := c.c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return res, fmt.Errorf("flags: %w", err)
		}
		present := map[uint32]bool{}
		for _, m := range msgs {
			u := uint32(m.UID)
			if _, ok := known[u]; !ok {
				continue
			}
			present[u] = true
			seen, flagged, answered := flags(m.Flags)
			st.SetFlags(mb.ID, u, seen, flagged, answered)
		}
		for u, id := range known {
			if !present[u] {
				st.DeleteMessage(id)
				res.Removed++
			}
		}
		res.Changed = len(present)
	} else if sel.NumMessages == 0 {
		for _, id := range known {
			st.DeleteMessage(id)
			res.Removed++
		}
	}

	// 2. New messages.
	if sel.NumMessages > 0 && (mb.LastUID == 0 || imap.UID(mb.LastUID+1) < sel.UIDNext || sel.UIDNext == 0) {
		opts := &imap.FetchOptions{UID: true, Flags: true, Envelope: true, RFC822Size: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}, BodySection: []*imap.FetchItemBodySection{refsSection}}
		var cmd *imapclient.FetchCommand
		if mb.LastUID == 0 {
			first := uint32(1)
			if o.Window > 0 && sel.NumMessages > uint32(o.Window) {
				first = sel.NumMessages - uint32(o.Window) + 1
			}
			var seq imap.SeqSet
			seq.AddRange(first, sel.NumMessages)
			cmd = c.c.Fetch(seq, opts)
		} else {
			var set imap.UIDSet
			set.AddRange(imap.UID(mb.LastUID+1), 0) // 0 = "*"
			cmd = c.c.Fetch(set, opts)
		}
		msgs, err := cmd.Collect()
		if err != nil {
			return res, fmt.Errorf("fetch new: %w", err)
		}
		last := mb.LastUID
		for _, m := range msgs {
			u := uint32(m.UID)
			if u <= mb.LastUID { // "n:*" returns the last message even when nothing is new
				continue
			}
			if _, err := st.PutMessage(summary(mb, m)); err != nil {
				return res, err
			}
			res.New++
			last = max(last, u)
		}
		mb.LastUID = last
	}
	mb.UIDValidity = sel.UIDValidity
	if err := st.SetSyncState(mb.ID, mb.UIDValidity, mb.LastUID); err != nil {
		return res, err
	}

	// 3. Bodies of the newest messages.
	if o.Prefetch > 0 {
		if err := c.prefetch(st, mb, o); err != nil {
			return res, err
		}
	}
	return res, nil
}

func flags(fs []imap.Flag) (seen, flagged, answered bool) {
	for _, f := range fs {
		switch f {
		case imap.FlagSeen:
			seen = true
		case imap.FlagFlagged:
			flagged = true
		case imap.FlagAnswered:
			answered = true
		}
	}
	return
}

func addrList(l []imap.Address) []msg.Address {
	var out []msg.Address
	for _, a := range l {
		if e := a.Addr(); e != "" {
			out = append(out, msg.Address{Name: a.Name, Email: e})
		}
	}
	return out
}

func summary(mb *store.Mailbox, m *imapclient.FetchMessageBuffer) *store.Message {
	s := &store.Message{AccountID: mb.AccountID, MailboxID: mb.ID, UID: uint32(m.UID), Size: m.RFC822Size}
	s.Seen, s.Flagged, s.Answered = flags(m.Flags)
	if e := m.Envelope; e != nil {
		s.Subject = e.Subject
		s.MessageID = strings.Trim(e.MessageID, "<>")
		if len(e.InReplyTo) > 0 {
			s.InReplyTo = strings.Trim(e.InReplyTo[0], "<>")
		}
		if f := addrList(e.From); len(f) > 0 {
			s.From = f[0]
		}
		s.To = addrList(e.To)
		s.Cc = addrList(e.Cc)
		s.Date = e.Date
	}
	if s.Date.IsZero() {
		s.Date = m.InternalDate
	}
	var refs []string
	if h := m.FindBodySection(refsSection); len(h) > 0 {
		refs = msgIDs(string(h))
	}
	s.ThreadKey = store.ThreadKey(s.MessageID, s.InReplyTo, refs)
	if m.BodyStructure != nil {
		m.BodyStructure.Walk(func(path []int, bs imap.BodyStructure) bool {
			if d := bs.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
				s.HasAttachments = true
			}
			return !s.HasAttachments
		})
	}
	return s
}

func msgIDs(h string) []string {
	var out []string
	for {
		i := strings.IndexByte(h, '<')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(h[i:], '>')
		if j < 0 {
			return out
		}
		out = append(out, h[i+1:i+j])
		h = h[i+j+1:]
	}
}

func (c *Conn) prefetch(st *store.Store, mb *store.Mailbox, o Options) error {
	list, err := st.List(store.Query{MailboxID: mb.ID, Limit: o.Prefetch})
	if err != nil {
		return err
	}
	var uids []imap.UID
	for _, m := range list {
		if !m.HasBody && (o.MaxPrefetchSize == 0 || m.Size <= o.MaxPrefetchSize) {
			uids = append(uids, imap.UID(m.UID))
		}
	}
	if len(uids) == 0 {
		return nil
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return c.fetchBodies(st, mb, uids)
}

var fullSection = &imap.FetchItemBodySection{Peek: true}

func (c *Conn) fetchBodies(st *store.Store, mb *store.Mailbox, uids []imap.UID) error {
	ids, err := st.UIDs(mb.ID)
	if err != nil {
		return err
	}
	msgs, err := c.c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{fullSection}}).Collect()
	if err != nil {
		return fmt.Errorf("fetch bodies: %w", err)
	}
	for _, m := range msgs {
		raw := m.FindBodySection(fullSection)
		id, ok := ids[uint32(m.UID)]
		if !ok || raw == nil {
			continue
		}
		if err := StoreBody(st, id, raw); err != nil {
			return err
		}
	}
	return nil
}

// StoreBody parses a raw message and saves it with its search text.
func StoreBody(st *store.Store, id int64, raw []byte) error {
	p, err := msg.Parse(raw)
	if err != nil {
		return st.SetBody(id, raw, "", "", false)
	}
	text := p.PlainText()
	hasAtt := false
	for _, a := range p.Attachments {
		if !a.Inline {
			hasAtt = true
		}
	}
	return st.SetBody(id, raw, text, msg.Snippet(text, 200), hasAtt)
}

// FetchBody downloads one message's body (when it is opened).
func (c *Conn) FetchBody(st *store.Store, mb *store.Mailbox, uid uint32) error {
	if _, err := c.Select(mb.Path); err != nil {
		return err
	}
	return c.fetchBodies(st, mb, []imap.UID{imap.UID(uid)})
}

// SetFlag adds or removes \Seen or \Flagged on the server.
func (c *Conn) SetFlag(mb *store.Mailbox, uid uint32, flag string, on bool) error {
	f := map[string]imap.Flag{"seen": imap.FlagSeen, "flagged": imap.FlagFlagged, "answered": imap.FlagAnswered}[flag]
	if f == "" {
		return fmt.Errorf("unknown flag %q", flag)
	}
	if _, err := c.Select(mb.Path); err != nil {
		return err
	}
	op := imap.StoreFlagsAdd
	if !on {
		op = imap.StoreFlagsDel
	}
	return c.c.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{f}}, nil).Close()
}

// Move moves a message to another folder (MOVE, or COPY + delete).
func (c *Conn) Move(from *store.Mailbox, uid uint32, to string) error {
	if _, err := c.Select(from.Path); err != nil {
		return err
	}
	_, err := c.c.Move(imap.UIDSetNum(imap.UID(uid)), to).Wait()
	return err
}

// Delete removes a message for good (used inside Trash).
func (c *Conn) Delete(from *store.Mailbox, uid uint32) error {
	if _, err := c.Select(from.Path); err != nil {
		return err
	}
	set := imap.UIDSetNum(imap.UID(uid))
	if err := c.c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return err
	}
	if c.c.Caps().Has(imap.CapUIDPlus) {
		return c.c.UIDExpunge(set).Close()
	}
	return c.c.Expunge().Close()
}

// Append saves a message into a folder (a sent copy, a draft).
func (c *Conn) Append(path string, raw []byte, flags ...imap.Flag) error {
	cmd := c.c.Append(path, int64(len(raw)), &imap.AppendOptions{Flags: flags})
	if _, err := cmd.Write(raw); err != nil {
		cmd.Close()
		return err
	}
	if err := cmd.Close(); err != nil {
		return err
	}
	_, err := cmd.Wait()
	return err
}
