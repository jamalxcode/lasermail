// Package msg parses RFC 5322 messages into what the app shows, and turns
// plain-text bodies into safe HTML.
package msg

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime"
	"net/mail"
	"regexp"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset" // non-UTF-8 charsets
	gomail "github.com/emersion/go-message/mail"
)

// Address is a display name and an address.
type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

func (a Address) String() string {
	return (&mail.Address{Name: a.Name, Address: a.Email}).String()
}

// Attachment describes one attached file; its bytes stay in the raw message.
type Attachment struct {
	Index     int    `json:"index"` // position among the attachments
	Name      string `json:"name"`
	Type      string `json:"type"`
	Size      int    `json:"size"`
	ContentID string `json:"content_id,omitempty"` // inline images referenced as cid:
	Inline    bool   `json:"inline"`
}

// Parsed is a message ready to show.
type Parsed struct {
	Subject     string       `json:"subject"`
	From        []Address    `json:"from"`
	To          []Address    `json:"to"`
	Cc          []Address    `json:"cc"`
	ReplyTo     []Address    `json:"reply_to"`
	Date        time.Time    `json:"date"`
	MessageID   string       `json:"message_id"`
	InReplyTo   string       `json:"in_reply_to"`
	References  []string     `json:"references"`
	Text        string       `json:"text"`
	HTML        string       `json:"html"` // raw, unsanitized
	Attachments []Attachment `json:"attachments"`
	// ListUnsubscribe is the https or mailto unsubscribe target, if any.
	ListUnsubscribe string `json:"list_unsubscribe,omitempty"`
	// WantsReadReceipt: the sender asked for a read receipt. Laser never
	// sends one; the app may say so.
	WantsReadReceipt bool `json:"wants_read_receipt"`
}

// MaxPart caps how much of one body part is read.
const MaxPart = 8 << 20

func addrs(h gomail.Header, key string) []Address {
	list, err := h.AddressList(key)
	if err != nil || len(list) == 0 {
		// Fall back to a lenient split for malformed headers.
		raw, _ := h.Text(key)
		if raw == "" {
			return nil
		}
		var out []Address
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				if a, err := mail.ParseAddress(p); err == nil {
					out = append(out, Address{a.Name, a.Address})
				} else {
					out = append(out, Address{Email: p})
				}
			}
		}
		return out
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		out = append(out, Address{Name: a.Name, Email: a.Address})
	}
	return out
}

var idRE = regexp.MustCompile(`<[^<>\s]+>`)

func msgIDs(v string) []string {
	m := idRE.FindAllString(v, -1)
	for i := range m {
		m[i] = strings.Trim(m[i], "<>")
	}
	return m
}

// Parse reads a whole message.
func Parse(raw []byte) (*Parsed, error) {
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if r == nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	defer r.Close()
	h := r.Header
	p := &Parsed{}
	p.Subject, _ = h.Subject()
	p.From = addrs(h, "From")
	p.To = addrs(h, "To")
	p.Cc = addrs(h, "Cc")
	p.ReplyTo = addrs(h, "Reply-To")
	p.Date, _ = h.Date()
	if ids := msgIDs(h.Get("Message-Id")); len(ids) > 0 {
		p.MessageID = ids[0]
	}
	if ids := msgIDs(h.Get("In-Reply-To")); len(ids) > 0 {
		p.InReplyTo = ids[0]
	}
	p.References = msgIDs(h.Get("References"))
	p.ListUnsubscribe = unsubscribe(h.Get("List-Unsubscribe"))
	p.WantsReadReceipt = h.Get("Disposition-Notification-To") != "" || h.Get("Return-Receipt-To") != ""

	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if part == nil {
				break // keep what we have from a damaged message
			}
		}
		switch ph := part.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := ph.ContentType()
			b, _ := io.ReadAll(io.LimitReader(part.Body, MaxPart))
			switch {
			case ct == "text/html" && p.HTML == "":
				p.HTML = string(b)
			case (ct == "text/plain" || ct == "") && p.Text == "":
				p.Text = string(b)
			case strings.HasPrefix(ct, "image/"):
				cid := strings.Trim(ph.Get("Content-Id"), "<> ")
				p.Attachments = append(p.Attachments, Attachment{Index: len(p.Attachments), Name: nameOf(ph.Header.Get("Content-Type"), ct), Type: ct, Size: len(b), ContentID: cid, Inline: true})
			}
		case *gomail.AttachmentHeader:
			name, _ := ph.Filename()
			ct, _, _ := ph.ContentType()
			n, _ := io.Copy(io.Discard, io.LimitReader(part.Body, 1<<30))
			cid := strings.Trim(ph.Get("Content-Id"), "<> ")
			if name == "" {
				name = nameOf(ph.Get("Content-Type"), ct)
			}
			p.Attachments = append(p.Attachments, Attachment{Index: len(p.Attachments), Name: name, Type: ct, Size: int(n), ContentID: cid, Inline: cid != "" && strings.HasPrefix(ct, "image/")})
		}
	}
	return p, nil
}

func nameOf(contentType, ct string) string {
	_, params, _ := mime.ParseMediaType(contentType)
	if n := params["name"]; n != "" {
		return n
	}
	ext := "bin"
	if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
		ext = strings.TrimPrefix(exts[0], ".")
	}
	return "attachment." + ext
}

// AttachmentData returns the decoded bytes of attachment i.
func AttachmentData(raw []byte, index int) (Attachment, []byte, error) {
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if r == nil {
		return Attachment{}, nil, err
	}
	defer r.Close()
	i := 0
	for {
		part, err := r.NextPart()
		if err == io.EOF || part == nil {
			break
		}
		var a Attachment
		switch ph := part.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := ph.ContentType()
			if !strings.HasPrefix(ct, "image/") {
				continue
			}
			a = Attachment{Name: nameOf(ph.Get("Content-Type"), ct), Type: ct, ContentID: strings.Trim(ph.Get("Content-Id"), "<> "), Inline: true}
		case *gomail.AttachmentHeader:
			ct, _, _ := ph.ContentType()
			name, _ := ph.Filename()
			if name == "" {
				name = nameOf(ph.Get("Content-Type"), ct)
			}
			a = Attachment{Name: name, Type: ct, ContentID: strings.Trim(ph.Get("Content-Id"), "<> ")}
		default:
			continue
		}
		if i == index {
			b, err := io.ReadAll(io.LimitReader(part.Body, 1<<30))
			a.Index, a.Size = i, len(b)
			return a, b, err
		}
		i++
	}
	return Attachment{}, nil, fmt.Errorf("no attachment %d", index)
}

func unsubscribe(v string) string {
	var mailto string
	for _, m := range regexp.MustCompile(`<([^>]+)>`).FindAllStringSubmatch(v, -1) {
		u := strings.TrimSpace(m[1])
		if strings.HasPrefix(u, "https://") {
			return u
		}
		if strings.HasPrefix(u, "mailto:") && mailto == "" {
			mailto = u
		}
	}
	return mailto
}

var (
	urlRE   = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)
	spaceRE = regexp.MustCompile(`\s+`)
	tagRE   = regexp.MustCompile(`(?s)<style.*?</style>|<script.*?</script>|<[^>]*>`)
)

// TextToHTML escapes plain text, links URLs and shows quoted lines (">") as
// blockquotes.
func TextToHTML(text string) string {
	var b strings.Builder
	depth := 0
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		d := 0
		for strings.HasPrefix(line, ">") {
			d++
			line = strings.TrimPrefix(strings.TrimPrefix(line, ">"), " ")
		}
		for ; depth < d; depth++ {
			b.WriteString(`<blockquote type="cite">`)
		}
		for ; depth > d; depth-- {
			b.WriteString("</blockquote>")
		}
		esc := html.EscapeString(line)
		esc = urlRE.ReplaceAllStringFunc(esc, func(u string) string {
			return `<a href="` + u + `">` + u + `</a>`
		})
		b.WriteString(esc)
		b.WriteString("<br>\n")
	}
	for ; depth > 0; depth-- {
		b.WriteString("</blockquote>")
	}
	return `<div style="white-space:pre-wrap;font-family:-apple-system,sans-serif">` + b.String() + `</div>`
}

// PlainText is the readable text of a message (for search and previews).
func (p *Parsed) PlainText() string {
	if strings.TrimSpace(p.Text) != "" {
		return p.Text
	}
	return html.UnescapeString(tagRE.ReplaceAllString(p.HTML, " "))
}

// Snippet is the first n characters of the text, quotes and signatures left
// out, on one line.
func Snippet(text string, n int) string {
	var keep []string
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, ">") {
			continue
		}
		if t == "--" || t == "-- " {
			break
		}
		keep = append(keep, t)
	}
	s := strings.TrimSpace(spaceRE.ReplaceAllString(strings.Join(keep, " "), " "))
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
