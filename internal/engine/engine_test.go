package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/jamalxcode/lasermail/internal/imapsync"
	"github.com/jamalxcode/lasermail/internal/msg"
	"github.com/jamalxcode/lasermail/internal/provider"
	"github.com/jamalxcode/lasermail/internal/secret"
	"github.com/jamalxcode/lasermail/internal/send"
	"github.com/jamalxcode/lasermail/internal/store"
)

type lit struct{ *bytes.Reader }

func (l lit) Size() int64 { return l.Reader.Size() }

func appendMsg(t *testing.T, u *imapmemserver.User, box, raw string, flags ...imap.Flag) {
	t.Helper()
	if _, err := u.Append(box, lit{bytes.NewReader([]byte(strings.ReplaceAll(raw, "\n", "\r\n")))}, &imap.AppendOptions{Flags: flags}); err != nil {
		t.Fatal(err)
	}
}

func message(from, subject, id, body string) string {
	return fmt.Sprintf("From: %s\nTo: me@example.test\nSubject: %s\nDate: %s\nMessage-ID: <%s>\nContent-Type: text/plain\n\n%s\n",
		from, subject, time.Now().Add(-time.Hour).Format(time.RFC1123Z), id, body)
}

const htmlMsg = "From: News <news@shop.test>\nTo: me@example.test\nSubject: Big sale\nMessage-ID: <sale@shop.test>\n" +
	"Content-Type: text/html\n\n<p>Sale!</p><img src=\"https://shop.list-manage.com/track/open.php?u=1\" width=1 height=1>" +
	"<img src=\"https://cdn.shop.test/hero.jpg\" width=600><a href=\"https://shop.test/?utm_source=mail&id=2\">Buy</a>\n"

type fixture struct {
	e        *Engine
	srv      *imapmemserver.Server
	gmail    *imapmemserver.User
	icloud   *imapmemserver.User
	smtp     *send.FakeServer
	imapAddr provider.Server
	smtpAddr provider.Server
}

func setup(t *testing.T) *fixture {
	f := &fixture{srv: imapmemserver.New()}
	f.gmail = imapmemserver.NewUser("me@gmail.test", "gpw")
	f.icloud = imapmemserver.NewUser("me@icloud.test", "ipw")
	for _, b := range []string{"INBOX", "Sent", "Trash", "All Mail"} {
		f.gmail.Create(b, nil)
	}
	for _, b := range []string{"INBOX", "Sent Messages", "Deleted Messages", "Archive", "Projects"} {
		f.icloud.Create(b, nil)
	}
	f.srv.AddUser(f.gmail)
	f.srv.AddUser(f.icloud)
	is := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return f.srv.NewSession(), nil, nil
		},
		InsecureAuth: true,
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go is.Serve(ln)
	t.Cleanup(func() { is.Close() })
	f.imapAddr = hostPort(ln.Addr().String())

	f.smtp, err = send.NewFakeServer("gpw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.smtp.Close)
	f.smtpAddr = hostPort(f.smtp.Addr)

	appendMsg(t, f.gmail, "INBOX", message("Ann <ann@a.test>", "Lunch?", "lunch@a.test", "Shall we meet at noon near the quarry?"))
	appendMsg(t, f.gmail, "INBOX", htmlMsg)
	appendMsg(t, f.gmail, "INBOX", message("Bob <bob@b.test>", "Old news", "old@b.test", "nothing"), imap.FlagSeen)
	appendMsg(t, f.icloud, "INBOX", message("Cat <cat@c.test>", "Photos", "photos@c.test", "Here are the photos"))
	appendMsg(t, f.icloud, "INBOX", message("Dan <dan@d.test>", "Invoice 42", "inv@d.test", "Please pay the invoice"))

	f.e, err = New(Config{DataDir: t.TempDir(), Secrets: secret.NewMemory(), FullSyncEvery: time.Minute, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.e.Close() })
	return f
}

func hostPort(addr string) provider.Server {
	h, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	return provider.Server{Host: h, Port: port, Security: provider.Insecure}
}

func (f *fixture) add(t *testing.T, email, pw string, savesSent bool) *store.Account {
	t.Helper()
	set := provider.Settings{IMAP: f.imapAddr, SMTP: f.smtpAddr, SavesSent: savesSent}
	a, err := f.e.AddAccount(context.Background(), NewAccount{Email: email, Name: "Me", Kind: provider.Custom, Password: pw, Settings: &set})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *fixture) list(q store.Query) []*store.Message {
	l, _ := f.e.List(q)
	return l
}

func find(l []*store.Message, subject string) *store.Message {
	for _, m := range l {
		if m.Subject == subject {
			return m
		}
	}
	return nil
}

// serverFlags reads a message's flags straight from the server.
func (f *fixture) serverFlags(t *testing.T, user, pw, box, subject string) []imap.Flag {
	c, err := imapsync.Dial(context.Background(), f.imapAddr, imapsync.Credentials{User: user, Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, _ := imapclient.DialInsecure(f.imapAddr.Addr(), nil)
	defer raw.Close()
	raw.Login(user, pw).Wait()
	raw.Select(box, nil).Wait()
	msgs, err := raw.Fetch(imap.SeqSet{imap.SeqRange{Start: 1, Stop: 0}}, &imap.FetchOptions{Flags: true, Envelope: true}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Envelope.Subject == subject {
			return m.Flags
		}
	}
	return nil
}

func (f *fixture) serverSubjects(t *testing.T, user, pw, box string) []string {
	raw, err := imapclient.DialInsecure(f.imapAddr.Addr(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.Login(user, pw).Wait()
	sel, err := raw.Select(box, nil).Wait()
	if err != nil || sel.NumMessages == 0 {
		return nil
	}
	msgs, _ := raw.Fetch(imap.SeqSet{imap.SeqRange{Start: 1, Stop: 0}}, &imap.FetchOptions{Envelope: true}).Collect()
	var out []string
	for _, m := range msgs {
		out = append(out, m.Envelope.Subject)
	}
	return out
}

func has(l []imap.Flag, f imap.Flag) bool {
	for _, x := range l {
		if x == f {
			return true
		}
	}
	return false
}

func TestTwoAccountsEndToEnd(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	events, stop := f.e.Subscribe()
	defer stop()

	if _, err := f.e.AddAccount(ctx, NewAccount{Email: "me@gmail.test", Kind: provider.Custom, Password: "wrong",
		Settings: &provider.Settings{IMAP: f.imapAddr, SMTP: f.smtpAddr}}); !errors.Is(err, imapsync.ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
	g := f.add(t, "me@gmail.test", "gpw", true)
	ic := f.add(t, "me@icloud.test", "ipw", false)

	// Unified inbox across both accounts.
	waitFor(t, "unified inbox", func() bool { return len(f.list(store.Query{Role: store.Inbox})) == 5 })
	if n := len(f.list(store.Query{Role: store.Inbox, Unread: true})); n != 4 {
		t.Fatalf("unread = %d", n)
	}
	if n := len(f.list(store.Query{AccountID: ic.ID, Role: store.Inbox})); n != 2 {
		t.Fatalf("icloud inbox = %d", n)
	}
	// Roles came from folder names (no SPECIAL-USE on this server).
	boxes, _ := f.e.Mailboxes(ic.ID)
	roles := map[string]string{}
	for _, b := range boxes {
		roles[b.Path] = b.Role
	}
	if roles["Sent Messages"] != store.Sent || roles["Deleted Messages"] != store.Trash || roles["Archive"] != store.Archive || roles["Projects"] != "" {
		t.Fatalf("roles %v", roles)
	}

	// Bodies were prefetched: body text is searchable.
	waitFor(t, "body search", func() bool { return len(f.list(store.Query{Text: "quarry"})) == 1 })
	if l := f.list(store.Query{Text: "invoice"}); len(l) != 1 || l[0].AccountID != ic.ID {
		t.Fatalf("search invoice: %v", l)
	}

	// Opening the newsletter: tracker and remote image blocked, link cleaned, marked read on the server.
	sale := find(f.list(store.Query{Role: store.Inbox}), "Big sale")
	v, err := f.e.Open(ctx, sale.ID, OpenOptions{MarkSeen: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Privacy.Trackers) != 1 || v.Privacy.RemoteBlocked != 1 || v.Privacy.LinksCleaned != 1 || v.Remote {
		t.Fatalf("privacy report %+v", v.Privacy)
	}
	if strings.Contains(v.HTML, "list-manage") || strings.Contains(v.HTML, "utm_source") || strings.Contains(v.HTML, ` src="https://cdn.shop.test`) {
		t.Fatalf("html leaks:\n%s", v.HTML)
	}
	if !has(f.serverFlags(t, "me@gmail.test", "gpw", "INBOX", "Big sale"), imap.FlagSeen) {
		t.Fatal("not marked read on the server")
	}
	// "Always load images from this sender".
	f.e.SetLoadImages("news@shop.test", true)
	v, _ = f.e.Open(ctx, sale.ID, OpenOptions{})
	if !v.Remote || !strings.Contains(v.HTML, ` src="https://cdn.shop.test/hero.jpg"`) || strings.Contains(v.HTML, "list-manage") {
		t.Fatalf("load images:\n%s", v.HTML)
	}

	// Flag on the server from another client shows up after a sync.
	lunch := find(f.list(store.Query{Role: store.Inbox}), "Lunch?")
	if err := f.e.SetFlag(ctx, lunch.ID, "flagged", true); err != nil {
		t.Fatal(err)
	}
	if !has(f.serverFlags(t, "me@gmail.test", "gpw", "INBOX", "Lunch?"), imap.FlagFlagged) {
		t.Fatal("flag not on server")
	}
	if n := len(f.list(store.Query{Flagged: true})); n != 1 {
		t.Fatalf("flagged view = %d", n)
	}

	// New mail arrives while idle: pushed, no polling.
	drain(events)
	appendMsg(t, f.icloud, "INBOX", message("Eve <eve@e.test>", "Pushed", "push@e.test", "hello there"))
	waitFor(t, "IDLE push", func() bool { return find(f.list(store.Query{Role: store.Inbox}), "Pushed") != nil })
	gotNew := false
	waitFor(t, "new_mail event", func() bool {
		for len(events) > 0 {
			if ev := <-events; ev.Type == "new_mail" && ev.AccountID == ic.ID {
				gotNew = true
			}
		}
		return gotNew
	})

	// Archive (iCloud has an Archive folder) and delete (to Trash).
	photos := find(f.list(store.Query{AccountID: ic.ID, Role: store.Inbox}), "Photos")
	if err := f.e.Archive(ctx, photos.ID); err != nil {
		t.Fatal(err)
	}
	if s := f.serverSubjects(t, "me@icloud.test", "ipw", "Archive"); len(s) != 1 || s[0] != "Photos" {
		t.Fatalf("archive on server: %v", s)
	}
	waitFor(t, "archived copy synced", func() bool { return find(f.list(store.Query{Role: store.Archive}), "Photos") != nil })
	old := find(f.list(store.Query{AccountID: g.ID, Role: store.Inbox}), "Old news")
	if err := f.e.Delete(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if s := f.serverSubjects(t, "me@gmail.test", "gpw", "Trash"); len(s) != 1 {
		t.Fatalf("trash on server: %v", s)
	}
	waitFor(t, "trash synced", func() bool { return find(f.list(store.Query{AccountID: g.ID, Role: store.Trash}), "Old news") != nil })
	// Deleting inside Trash is for good.
	trashed := find(f.list(store.Query{AccountID: g.ID, Role: store.Trash}), "Old news")
	if err := f.e.Delete(ctx, trashed.ID); err != nil {
		t.Fatal(err)
	}
	if s := f.serverSubjects(t, "me@gmail.test", "gpw", "Trash"); len(s) != 0 {
		t.Fatalf("trash after delete: %v", s)
	}

	// Reply, then send: SMTP gets it, iCloud files a copy in Sent, Gmail doesn't.
	d, acct, err := f.e.Reply(ctx, lunch.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if acct != g.ID || d.Subject != "Re: Lunch?" || d.To[0].Email != "ann@a.test" || d.InReplyTo != "lunch@a.test" ||
		!strings.Contains(d.Text, "> Shall we meet") || len(d.Cc) != 1 || d.Cc[0].Email != "me@example.test" {
		t.Fatalf("reply draft %+v", d)
	}
	d.Text = "Yes!" + d.Text
	f.smtp.Secret = "gpw"
	if err := f.e.Send(ctx, g.ID, d); err != nil {
		t.Fatal(err)
	}
	got := f.smtp.Received()
	if len(got) != 1 || got[0].From != "me@gmail.test" || !strings.Contains(got[0].Data, "In-Reply-To: <lunch@a.test>") {
		t.Fatalf("smtp got %+v", got)
	}
	if s := f.serverSubjects(t, "me@gmail.test", "gpw", "Sent"); len(s) != 0 {
		t.Fatalf("gmail-like account should not append to Sent: %v", s)
	}
	if !has(f.serverFlags(t, "me@gmail.test", "gpw", "INBOX", "Lunch?"), imap.FlagAnswered) {
		t.Fatal("replied message not marked answered")
	}

	f.smtp.Secret = "ipw"
	err = f.e.Send(ctx, ic.ID, &msg.Draft{To: []msg.Address{{Email: "x@y.test"}}, Bcc: []msg.Address{{Email: "hidden@y.test"}}, Subject: "From iCloud", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if s := f.serverSubjects(t, "me@icloud.test", "ipw", "Sent Messages"); len(s) != 1 || s[0] != "From iCloud" {
		t.Fatalf("icloud sent copy: %v", s)
	}
	last := f.smtp.Received()[1]
	if len(last.To) != 2 || strings.Contains(last.Data, "hidden@y.test") {
		t.Fatalf("bcc handling: %+v", last)
	}

	// Forward keeps attachments; saving an attachment never overwrites.
	appendMsg(t, f.icloud, "INBOX", "From: f@f.test\nSubject: File\nMessage-ID: <file@f.test>\nContent-Type: multipart/mixed; boundary=B\n\n--B\nContent-Type: text/plain\n\nsee file\n--B\nContent-Type: text/plain\nContent-Disposition: attachment; filename=\"../../evil.txt\"\n\ndata\n--B--\n")
	f.e.SyncNow(ic.ID)
	waitFor(t, "file message", func() bool { return find(f.list(store.Query{Role: store.Inbox}), "File") != nil })
	file := find(f.list(store.Query{Role: store.Inbox}), "File")
	dir := t.TempDir()
	p1, err := f.e.SaveAttachment(ctx, file.ID, 0, dir)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := f.e.SaveAttachment(ctx, file.ID, 0, dir)
	if p1 != dir+"/evil.txt" || p2 != dir+"/evil 2.txt" {
		t.Fatalf("attachment paths %s %s", p1, p2)
	}
	if b, _ := os.ReadFile(p1); strings.TrimSpace(string(b)) != "data" {
		t.Fatalf("attachment content %q", b)
	}
	fd, _, err := f.e.Forward(ctx, file.ID)
	if err != nil || fd.Subject != "Fwd: File" || len(fd.Files) != 1 {
		t.Fatalf("forward %+v %v", fd, err)
	}

	// Removing an account forgets everything about it.
	if err := f.e.RemoveAccount(ic.ID); err != nil {
		t.Fatal(err)
	}
	if l := f.list(store.Query{AccountID: ic.ID}); len(l) != 0 {
		t.Fatalf("leftovers %d", len(l))
	}
	if _, err := f.e.secrets.Get(passwordKey(ic.ID)); !errors.Is(err, secret.ErrNotFound) {
		t.Fatal("password left in the secret store")
	}
	if st := f.e.Status()[g.ID]; st.State == "error" {
		t.Fatalf("gmail account status: %+v", st)
	}
}

func TestRestartKeepsAccounts(t *testing.T) {
	f := setup(t)
	f.add(t, "me@gmail.test", "gpw", true)
	waitFor(t, "sync", func() bool { return len(f.list(store.Query{Role: store.Inbox})) == 3 })
	dir := f.e.cfg.DataDir
	secrets := f.e.secrets
	f.e.Close()
	appendMsg(t, f.gmail, "INBOX", message("Zed <z@z.test>", "While away", "away@z.test", "x"))
	e2, err := New(Config{DataDir: dir, Secrets: secrets, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	f.e = e2
	waitFor(t, "catch up after restart", func() bool { return find(f.list(store.Query{Role: store.Inbox}), "While away") != nil })
	if n := len(f.list(store.Query{Role: store.Inbox})); n != 4 {
		t.Fatalf("after restart %d (duplicates?)", n)
	}
}

func drain(ch <-chan Event) {
	for len(ch) > 0 {
		<-ch
	}
}

func TestDemoParsesCleanly(t *testing.T) {
	e, err := New(Config{DataDir: t.TempDir(), Secrets: secret.NewMemory(), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.SeedDemo(time.Now()); err != nil {
		t.Fatal(err)
	}
	l, _ := e.List(store.Query{Role: store.Inbox})
	for _, m := range l {
		raw, err := e.Store().Body(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		p, err := msg.Parse(raw)
		if err != nil || p.Subject != m.Subject {
			t.Errorf("demo message %q parses as %q (%v)", m.Subject, p.Subject, err)
		}
	}
	d, _, err := e.Reply(context.Background(), l[0].ID, false)
	if err != nil || strings.Contains(d.Text, `"Priya`) || !strings.Contains(d.Text, "Priya Natarajan <priya@northwind.example> wrote:") {
		t.Fatalf("reply header: %q %v", d.Text, err)
	}
}

func TestFirstSyncIsNotNewMail(t *testing.T) {
	f := setup(t)
	events, stop := f.e.Subscribe()
	defer stop()
	f.add(t, "me@gmail.test", "gpw", true)
	waitFor(t, "first sync", func() bool { return len(f.list(store.Query{Role: store.Inbox})) == 3 })
	time.Sleep(200 * time.Millisecond)
	for len(events) > 0 {
		if ev := <-events; ev.Type == "new_mail" {
			t.Fatalf("first sync announced new mail: %+v", ev)
		}
	}
}

// iCloud: IMAP wants the name part of the address; the password was pasted
// with invisible characters. Both server password forms must work.
func TestICloudSignIn(t *testing.T) {
	for _, serverPW := range []string{"abcd-efgh-ijkl-mnop", "abcdefghijklmnop"} {
		f := setup(t)
		u := imapmemserver.NewUser("yodabasket", serverPW) // the full address is not a valid login here
		u.Create("INBOX", nil)
		appendMsg(t, u, "INBOX", message("Ann <ann@a.test>", "Hello iCloud", "hi@a.test", "hi"))
		f.srv.AddUser(u)
		set := provider.Settings{IMAP: f.imapAddr, SMTP: f.smtpAddr}
		a, err := f.e.AddAccount(context.Background(), NewAccount{Email: "yodabasket@icloud.com", Name: "yoda",
			Kind: provider.ICloud, Password: "\u00a0abcd-efgh-ijkl-mnop\u200b\n", Settings: &set})
		if err != nil {
			t.Fatalf("server password %q: %v", serverPW, err)
		}
		if a.Settings.IMAPUser != "yodabasket" {
			t.Fatalf("saved user %q", a.Settings.IMAPUser)
		}
		if pw, _ := f.e.secrets.Get(passwordKey(a.ID)); pw != serverPW {
			t.Fatalf("saved password %q, server has %q", pw, serverPW)
		}
		waitFor(t, "icloud inbox", func() bool {
			return find(f.list(store.Query{AccountID: a.ID, Role: store.Inbox}), "Hello iCloud") != nil
		})
		// Sending signs in to SMTP with the full address.
		f.smtp.Secret = serverPW
		if err := f.e.Send(context.Background(), a.ID, &msg.Draft{To: []msg.Address{{Email: "x@y.test"}}, Subject: "s", Text: "t"}); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	// A truly wrong password says what to check.
	f := setup(t)
	u := imapmemserver.NewUser("yodabasket", "right")
	u.Create("INBOX", nil)
	f.srv.AddUser(u)
	set := provider.Settings{IMAP: f.imapAddr, SMTP: f.smtpAddr}
	_, err := f.e.AddAccount(context.Background(), NewAccount{Email: "yodabasket@icloud.com", Kind: provider.ICloud, Password: "wrong", Settings: &set})
	if !errors.Is(err, imapsync.ErrAuth) || !strings.Contains(err.Error(), "isn't an app-specific password") || !strings.Contains(err.Error(), "(5 characters)") {
		t.Fatalf("wrong password: %v", err)
	}
	// Shaped like an app-specific password but refused: the general advice.
	_, err = f.e.AddAccount(context.Background(), NewAccount{Email: "yodabasket@icloud.com", Kind: provider.ICloud, Password: "abcd-efgh-ijkl-mnop", Settings: &set})
	if !errors.Is(err, imapsync.ErrAuth) || !strings.Contains(err.Error(), "iCloud refused") {
		t.Fatalf("wrong app-specific password: %v", err)
	}
}

// googleLike refuses every login the way Gmail does for a non-app password.
type googleLike struct{ imapserver.Session }

func (googleLike) Login(string, string) error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Code: "ALERT",
		Text: "Application-specific password required: https://support.google.com/accounts/answer/185833 (Failure)"}
}

func TestGmailAppPasswordMessage(t *testing.T) {
	mem := imapmemserver.New()
	is := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return googleLike{mem.NewSession()}, nil, nil
		},
		InsecureAuth: true,
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go is.Serve(ln)
	defer is.Close()
	_, _, err = tryLogin(context.Background(), provider.Settings{IMAP: hostPort(ln.Addr().String())}, provider.Gmail, "a@gmail.com", "my google password")
	if !errors.Is(err, imapsync.ErrAuth) || !strings.Contains(err.Error(), "not your Google Account password") {
		t.Fatalf("got %v", err)
	}
}
