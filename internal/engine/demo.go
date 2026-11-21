package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/jamalxcode/laser-email/internal/imapsync"
	"github.com/jamalxcode/laser-email/internal/msg"
	"github.com/jamalxcode/laser-email/internal/provider"
	"github.com/jamalxcode/laser-email/internal/store"
)

// SeedDemo fills an empty store with two made-up accounts and some mail, so
// the app can be shown (and screenshotted in CI) without a real account.
// Use it with Config.Offline.
func (e *Engine) SeedDemo(now time.Time) error {
	if accts, _ := e.st.Accounts(); len(accts) > 0 {
		return nil
	}
	g, _ := provider.Preset(provider.Gmail)
	i, _ := provider.Preset(provider.ICloud)
	work := &store.Account{Email: "sam.rivera@gmail.com", Name: "Sam Rivera", Label: "Gmail", Auth: provider.Password, Settings: g}
	home := &store.Account{Email: "sam@icloud.com", Name: "Sam Rivera", Label: "iCloud", Auth: provider.Password, Settings: i}
	for _, a := range []*store.Account{work, home} {
		if err := e.st.AddAccount(a); err != nil {
			return err
		}
		e.status[a.ID] = &AccountStatus{State: "idle", LastSync: now}
	}
	boxes := map[int64]map[string]*store.Mailbox{}
	for _, a := range []*store.Account{work, home} {
		boxes[a.ID] = map[string]*store.Mailbox{}
		names := map[string]string{store.Inbox: "INBOX", store.Drafts: "Drafts", store.Sent: "Sent", store.Junk: "Junk", store.Trash: "Trash", store.Archive: "Archive"}
		if a == work {
			names[store.Sent], names[store.Trash], names[store.Junk], names[store.Drafts] = "[Gmail]/Sent Mail", "[Gmail]/Trash", "[Gmail]/Spam", "[Gmail]/Drafts"
			delete(names, store.Archive)
		}
		for role, path := range names {
			title := map[string]string{store.Inbox: "Inbox", store.Drafts: "Drafts", store.Sent: "Sent", store.Junk: "Junk", store.Trash: "Trash", store.Archive: "Archive"}[role]
			mb, err := e.st.UpsertMailbox(a.ID, path, title, role)
			if err != nil {
				return err
			}
			boxes[a.ID][role] = mb
		}
	}
	p, _ := e.st.UpsertMailbox(home.ID, "Projects", "Projects", store.Other)
	boxes[home.ID]["projects"] = p

	type demo struct {
		acct                *store.Account
		box                 string
		fromName, fromEmail string
		subject, body       string
		ago                 time.Duration
		seen, flagged, att  bool
		html                string
	}
	mails := []demo{
		{work, store.Inbox, "Priya Natarajan", "priya@northwind.example", "Q4 roadmap review — moved to Thursday",
			"Hi Sam,\n\nWe're moving the Q4 roadmap review to Thursday at 10:00. I've attached the updated deck; the main change is the timeline for the sync engine.\n\nCould you bring numbers on the beta signups?\n\nThanks,\nPriya", 25 * time.Minute, false, true, true, ""},
		{home, store.Inbox, "Mom", "rivera.family@icloud.com", "Sunday dinner 🍝",
			"Are you still coming on Sunday? Dad is making his lasagna. Bring the photos from the trip!\n\nLove,\nMom", 52 * time.Minute, false, false, false, ""},
		{work, store.Inbox, "Shopwise Deals", "deals@shopwise.example", "Your weekend picks are here",
			"", 2 * time.Hour, false, false, false,
			`<table width="100%" style="font-family:-apple-system,Helvetica,sans-serif"><tr><td style="padding:24px;background:#0b5cff;color:#fff;font-size:22px"><b>Weekend picks for you</b></td></tr>` +
				`<tr><td style="padding:24px"><img src="https://cdn.shopwise.example/hero.jpg" width="560" alt="Sale banner"><p>Up to 40% off headphones, chargers and more.</p>` +
				`<p><a href="https://shopwise.example/deals?utm_source=email&utm_campaign=weekend&mc_eid=8812&id=77">Shop the sale</a></p></td></tr></table>` +
				`<img src="https://shopwise.list-manage.com/track/open.php?u=abc&id=77" width="1" height="1"><img src="https://ct.sendgrid.net/wf/open?upn=xyz" width="1" height="1">`},
		{home, store.Inbox, "Jordan Lee", "jordan.lee@fastmail.example", "Re: Climbing on Saturday?",
			"I'm in! Let's meet at the gym at 9. I'll bring the spare harness.\n\n> Anyone up for climbing this Saturday?", 5 * time.Hour, true, false, false, ""},
		{work, store.Inbox, "GitHub", "noreply@github.example", "[laser-email] CI passed on main",
			"All checks have passed on main.\n\n✓ test (ubuntu-latest)\n✓ test (macos-latest)\n✓ app", 9 * time.Hour, true, false, false, ""},
		{home, store.Inbox, "Bay Area Transit", "alerts@transit.example", "Service change on the Blue Line",
			"Weekend track work: trains every 20 minutes between Downtown and Airport stations.", 26 * time.Hour, true, false, false, ""},
		{work, store.Inbox, "Alex Chen", "alex@northwind.example", "Design review notes",
			"Notes from today's review are in the doc. Two open questions: the sidebar badge style and the compose window size.", 30 * time.Hour, false, false, false, ""},
		{home, store.Inbox, "Library", "notices@citylibrary.example", "Your hold is ready for pickup",
			"\"The Overstory\" is ready at the Main branch until Friday.", 3 * 24 * time.Hour, true, false, false, ""},
		{work, store.Inbox, "Payroll", "payroll@northwind.example", "Your November payslip",
			"Your payslip for November is available. It is attached as a PDF.", 4 * 24 * time.Hour, true, false, true, ""},
		{home, "projects", "Contractor", "build@renovate.example", "Kitchen quote",
			"Attached is the quote for the kitchen. We can start in January.", 6 * 24 * time.Hour, true, true, true, ""},
		{work, store.Sent, "Sam Rivera", "sam.rivera@gmail.com", "Re: Q4 roadmap review",
			"Thursday works for me. I'll bring the beta numbers.", 20 * time.Minute, true, false, false, ""},
	}
	for n, d := range mails {
		mb := boxes[d.acct.ID][d.box]
		id := fmt.Sprintf("demo%d@laser.example", n)
		var b strings.Builder
		fmt.Fprintf(&b, "From: %q <%s>\r\nTo: %q <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s>\r\nMIME-Version: 1.0\r\n",
			d.fromName, d.fromEmail, d.acct.Name, d.acct.Email, mimeWord(d.subject), now.Add(-d.ago).Format(time.RFC1123Z), id)
		if d.html != "" {
			b.WriteString("List-Unsubscribe: <https://shopwise.example/unsubscribe?u=1>\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + d.html + "\r\n")
		} else if d.att {
			b.WriteString("Content-Type: multipart/mixed; boundary=LSR\r\n\r\n--LSR\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
				strings.ReplaceAll(d.body, "\n", "\r\n") + "\r\n--LSR\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"" +
				map[bool]string{true: "Q4 Roadmap.pdf", false: "document.pdf"}[n == 0] + "\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n--LSR--\r\n")
		} else {
			b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n" + strings.ReplaceAll(d.body, "\n", "\r\n") + "\r\n")
		}
		m := &store.Message{AccountID: d.acct.ID, MailboxID: mb.ID, UID: uint32(n + 1), MessageID: id, ThreadKey: id, Subject: d.subject,
			From: msg.Address{Name: d.fromName, Email: d.fromEmail}, To: []msg.Address{{Name: d.acct.Name, Email: d.acct.Email}},
			Date: now.Add(-d.ago), Seen: d.seen, Flagged: d.flagged, HasAttachments: d.att, Size: int64(b.Len())}
		rid, err := e.st.PutMessage(m)
		if err != nil {
			return err
		}
		if err := imapsync.StoreBody(e.st, rid, []byte(b.String())); err != nil {
			return err
		}
		e.st.SetSyncState(mb.ID, 1, uint32(n+1))
	}
	return nil
}

func mimeWord(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?Q?" + strings.NewReplacer("=", "=3D", "?", "=3F", "_", "=5F", " ", "_").Replace(qp(s)) + "?="
		}
	}
	return s
}

func qp(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c > 127 {
			fmt.Fprintf(&b, "=%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
