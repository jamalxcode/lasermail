package msg

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

// Draft is an outgoing message.
type Draft struct {
	From       Address   `json:"from"`
	To         []Address `json:"to"`
	Cc         []Address `json:"cc"`
	Bcc        []Address `json:"bcc"`
	Subject    string    `json:"subject"`
	Text       string    `json:"text"`
	InReplyTo  string    `json:"in_reply_to,omitempty"`
	References []string  `json:"references,omitempty"`
	Files      []File    `json:"files,omitempty"`
}

// File to attach.
type File struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data []byte `json:"data"`
}

// Recipients is every envelope recipient, Bcc included.
func (d *Draft) Recipients() []string {
	var out []string
	for _, l := range [][]Address{d.To, d.Cc, d.Bcc} {
		for _, a := range l {
			out = append(out, a.Email)
		}
	}
	return out
}

func toMail(l []Address) []*gomail.Address {
	out := make([]*gomail.Address, 0, len(l))
	for _, a := range l {
		out = append(out, &gomail.Address{Name: a.Name, Address: a.Email})
	}
	return out
}

// NewMessageID makes a random id on the sender's own domain, so it says
// nothing about this Mac (no host name, no time, no app name).
func NewMessageID(from string) string {
	b := make([]byte, 16)
	rand.Read(b)
	domain := "localhost"
	if at := strings.LastIndexByte(from, '@'); at >= 0 {
		domain = from[at+1:]
	}
	return hex.EncodeToString(b) + "@" + domain
}

// Build writes the RFC 5322 message. Privacy: the Date is in UTC (no time
// zone hint), there is no User-Agent/X-Mailer, the Message-ID is random, and
// Bcc never appears in the headers.
func Build(d *Draft, now time.Time) (raw []byte, messageID string, err error) {
	var h gomail.Header
	h.SetDate(now.UTC().Truncate(time.Minute))
	h.SetAddressList("From", toMail([]Address{d.From}))
	h.SetAddressList("To", toMail(d.To))
	if len(d.Cc) > 0 {
		h.SetAddressList("Cc", toMail(d.Cc))
	}
	h.SetSubject(d.Subject)
	messageID = NewMessageID(d.From.Email)
	h.SetMessageID(messageID)
	if d.InReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{d.InReplyTo})
	}
	if len(d.References) > 0 {
		h.SetMsgIDList("References", d.References)
	}
	var buf bytes.Buffer
	text := strings.ReplaceAll(strings.ReplaceAll(d.Text, "\r\n", "\n"), "\n", "\r\n")
	if len(d.Files) == 0 {
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		w, err := gomail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, "", err
		}
		w.Write([]byte(text))
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), messageID, nil
	}
	mw, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, "", err
	}
	tw, err := mw.CreateInline()
	if err != nil {
		return nil, "", err
	}
	var th gomail.InlineHeader
	th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	pw, err := tw.CreatePart(th)
	if err != nil {
		return nil, "", err
	}
	pw.Write([]byte(text))
	pw.Close()
	tw.Close()
	for _, f := range d.Files {
		var ah gomail.AttachmentHeader
		ct := f.Type
		if ct == "" {
			ct = "application/octet-stream"
		}
		ah.SetContentType(ct, nil)
		ah.SetFilename(f.Name)
		w, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, "", err
		}
		w.Write(f.Data)
		w.Close()
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), messageID, nil
}

// ReplySubject adds "Re: " once.
func ReplySubject(s string) string {
	t := strings.TrimSpace(s)
	l := strings.ToLower(t)
	if strings.HasPrefix(l, "re:") || strings.HasPrefix(l, "aw:") || strings.HasPrefix(l, "sv:") {
		return t
	}
	return "Re: " + t
}

// ForwardSubject adds "Fwd: " once.
func ForwardSubject(s string) string {
	t := strings.TrimSpace(s)
	l := strings.ToLower(t)
	if strings.HasPrefix(l, "fwd:") || strings.HasPrefix(l, "fw:") {
		return t
	}
	return "Fwd: " + t
}
