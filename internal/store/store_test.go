package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jamalxcode/laser-email/internal/msg"
	"github.com/jamalxcode/laser-email/internal/provider"
)

func open(t *testing.T) *Store {
	s, err := Open(filepath.Join(t.TempDir(), "laser.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUnifiedInboxAndSearch(t *testing.T) {
	s := open(t)
	g, _ := provider.Preset(provider.Gmail)
	i, _ := provider.Preset(provider.ICloud)
	a1 := &Account{Email: "me@gmail.com", Auth: provider.Password, Settings: g}
	a2 := &Account{Email: "me@icloud.com", Auth: provider.Password, Settings: i}
	for _, a := range []*Account{a1, a2} {
		if err := s.AddAccount(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddAccount(&Account{Email: "ME@gmail.com", Settings: g}); err == nil {
		t.Fatal("duplicate account accepted")
	}
	in1, _ := s.UpsertMailbox(a1.ID, "INBOX", "Inbox", Inbox)
	in2, _ := s.UpsertMailbox(a2.ID, "INBOX", "Inbox", Inbox)
	sent, _ := s.UpsertMailbox(a2.ID, "Sent Messages", "Sent", Sent)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	put := func(acct, mbox int64, uid uint32, subj string, h int, seen bool) int64 {
		id, err := s.PutMessage(&Message{AccountID: acct, MailboxID: mbox, UID: uid, Subject: subj, MessageID: subj + "@x",
			From: msg.Address{Name: "Ann", Email: "Ann@Example.com"}, Date: base.Add(time.Duration(h) * time.Hour), Seen: seen})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	put(a1.ID, in1.ID, 1, "gmail one", 1, false)
	put(a2.ID, in2.ID, 1, "icloud one", 2, true)
	id3 := put(a1.ID, in1.ID, 2, "gmail two", 3, false)
	put(a2.ID, sent.ID, 1, "sent thing", 4, true)

	l, err := s.List(Query{Role: Inbox})
	if err != nil || len(l) != 3 || l[0].Subject != "gmail two" || l[2].Subject != "gmail one" {
		t.Fatalf("unified inbox: %v %v", titles(l), err)
	}
	if l[0].From.Email != "ann@example.com" {
		t.Fatalf("from not lower-cased: %q", l[0].From.Email)
	}
	l, _ = s.List(Query{Role: Inbox, Unread: true})
	if len(l) != 2 {
		t.Fatalf("unread: %v", titles(l))
	}
	l, _ = s.List(Query{AccountID: a2.ID, Role: Inbox})
	if len(l) != 1 {
		t.Fatalf("per account: %v", titles(l))
	}
	l, _ = s.List(Query{Role: Inbox, Before: base.Add(3 * time.Hour), Limit: 1})
	if len(l) != 1 || l[0].Subject != "icloud one" {
		t.Fatalf("paging: %v", titles(l))
	}
	c, _ := s.Counts()
	if c[Inbox] != 2 {
		t.Fatalf("counts %v", c)
	}

	// Flags update in place.
	put(a1.ID, in1.ID, 2, "gmail two", 3, true)
	if m, _ := s.Message(id3); !m.Seen {
		t.Fatal("flag not updated")
	}

	// Search: subject, sender, then body after SetBody; operators can't break it.
	if err := s.SetBody(id3, []byte("raw"), "the quarterly invoice is attached", "the quarterly…", true); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"invoice": 1, "quart": 1, "gmail": 2, "ann": 4, `"inv OR (x`: 0, "zzz": 0} {
		l, err := s.List(Query{Text: q})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if q == "ann" {
			want = 4
		}
		if len(l) != want {
			t.Errorf("search %q = %v, want %d", q, titles(l), want)
		}
	}
	if b, err := s.Body(id3); err != nil || string(b) != "raw" {
		t.Fatal("body")
	}

	// UIDVALIDITY reset and account removal cascade.
	if err := s.ResetMailbox(in1.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.List(Query{MailboxID: in1.ID}); len(l) != 0 {
		t.Fatal("reset left messages")
	}
	if l, _ := s.List(Query{Text: "invoice"}); len(l) != 0 {
		t.Fatal("search index kept a deleted message")
	}
	s.DeleteAccount(a2.ID)
	if l, _ := s.List(Query{}); len(l) != 0 {
		t.Fatalf("cascade: %v", titles(l))
	}
}

func TestSenderPrefs(t *testing.T) {
	s := open(t)
	if s.LoadImages("a@b.c") {
		t.Fatal("default should be off")
	}
	s.SetLoadImages("A@b.c", true)
	if !s.LoadImages("a@B.c") {
		t.Fatal("not remembered")
	}
}

func titles(l []*Message) []string {
	var out []string
	for _, m := range l {
		out = append(out, m.Subject)
	}
	return out
}
