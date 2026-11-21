package msg

import (
	"strings"
	"testing"
	"time"
)

const sample = "From: \"Ann Example\" <ann@example.com>\r\n" +
	"To: bob@example.org, \"Cé\" <ce@example.org>\r\n" +
	"Subject: =?UTF-8?B?SGVsbG8g8J+Riw==?=\r\n" +
	"Date: Tue, 03 Nov 2026 10:00:00 +0300\r\n" +
	"Message-ID: <abc@example.com>\r\n" +
	"References: <r1@x> <r2@x>\r\n" +
	"In-Reply-To: <r2@x>\r\n" +
	"List-Unsubscribe: <mailto:u@example.com>, <https://example.com/u?id=1>\r\n" +
	"Disposition-Notification-To: ann@example.com\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=XX\r\n\r\n" +
	"--XX\r\nContent-Type: multipart/alternative; boundary=YY\r\n\r\n" +
	"--YY\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nCaf=E9 at 9\r\n> quoted\r\n-- \r\nsig\r\n" +
	"--YY\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Caf\xc3\xa9 at 9</p>\r\n--YY--\r\n" +
	"--XX\r\nContent-Type: application/pdf; name=\"a.pdf\"\r\nContent-Disposition: attachment; filename=\"a.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0x\r\n--XX--\r\n"

func TestParse(t *testing.T) {
	p, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != "Hello 👋" || p.From[0].Name != "Ann Example" || len(p.To) != 2 || p.To[1].Name != "Cé" {
		t.Fatalf("headers: %+v", p)
	}
	if p.MessageID != "abc@example.com" || p.InReplyTo != "r2@x" || len(p.References) != 2 {
		t.Fatalf("ids: %+v", p)
	}
	if !strings.HasPrefix(p.Text, "Café at 9") || !strings.Contains(p.HTML, "Café") {
		t.Fatalf("bodies: %q %q", p.Text, p.HTML)
	}
	if len(p.Attachments) != 1 || p.Attachments[0].Name != "a.pdf" || p.Attachments[0].Size != 6 {
		t.Fatalf("attachments: %+v", p.Attachments)
	}
	if p.ListUnsubscribe != "https://example.com/u?id=1" || !p.WantsReadReceipt {
		t.Fatalf("unsubscribe/receipt: %+v", p)
	}
	if s := Snippet(p.PlainText(), 100); s != "Café at 9" {
		t.Fatalf("snippet %q", s)
	}
	a, data, err := AttachmentData([]byte(sample), 0)
	if err != nil || a.Name != "a.pdf" || string(data) != "%PDF-1" {
		t.Fatalf("attachment data: %+v %q %v", a, data, err)
	}
}

func TestBuildPrivacy(t *testing.T) {
	d := &Draft{
		From: Address{"Me", "me@icloud.com"}, To: []Address{{Email: "a@example.com"}},
		Bcc: []Address{{Email: "secret@example.com"}}, Subject: "Hi", Text: "line1\nline2",
		InReplyTo: "x@y", References: []string{"x@y"},
	}
	raw, id, err := Build(d, time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("AST", 3*3600)))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, bad := range []string{"secret@example.com", "X-Mailer", "User-Agent", "+0300"} {
		if strings.Contains(s, bad) {
			t.Errorf("message leaks %q:\n%s", bad, s)
		}
	}
	if !strings.HasSuffix(id, "@icloud.com") || !strings.Contains(s, "+0000") || !strings.Contains(s, "line1\r\nline2") {
		t.Fatalf("message:\n%s", s)
	}
	if got := d.Recipients(); len(got) != 2 {
		t.Fatalf("recipients %v", got)
	}
	p, err := Parse(raw)
	if err != nil || p.Subject != "Hi" || p.InReplyTo != "x@y" {
		t.Fatalf("round trip: %+v %v", p, err)
	}
	d.Files = []File{{Name: "n.txt", Type: "text/plain", Data: []byte("hello")}}
	raw, _, _ = Build(d, time.Now())
	if _, b, err := AttachmentData(raw, 0); err != nil || string(b) != "hello" {
		t.Fatalf("attachment round trip %q %v", b, err)
	}
}

func TestTextToHTML(t *testing.T) {
	h := TextToHTML("see https://example.com/a.\n> q <b>\n>> qq\nend")
	for _, want := range []string{`<a href="https://example.com/a">`, `<blockquote type="cite">q &lt;b&gt;`, "qq", "</blockquote></blockquote>end<br>"} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in %s", want, h)
		}
	}
}

func TestSubjects(t *testing.T) {
	if ReplySubject("Re: x") != "Re: x" || ReplySubject("x") != "Re: x" || ForwardSubject("Fwd: x") != "Fwd: x" {
		t.Fatal("subjects")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(sample))
	f.Fuzz(func(t *testing.T, b []byte) { Parse(b) })
}
