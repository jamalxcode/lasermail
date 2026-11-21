// Package privacy makes incoming HTML mail safe to show and keeps it from
// reporting back: scripts, forms, frames and every remote load are removed,
// tracking pixels are dropped even when remote images are allowed, and links
// lose their tracking parameters and redirect wrappers.
package privacy

import (
	"bytes"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Options for Sanitize.
type Options struct {
	// AllowRemoteImages loads remote images (the user chose "Load Images" for
	// this message or sender). Known trackers and pixels stay blocked.
	AllowRemoteImages bool
}

// Report says what was removed, for the "Laser blocked…" banner.
type Report struct {
	Trackers      []string `json:"trackers"`       // tracker hosts or "pixel"
	RemoteBlocked int      `json:"remote_blocked"` // remote images/styles not loaded
	LinksCleaned  int      `json:"links_cleaned"`  // links stripped of tracking
	Removed       int      `json:"removed"`        // scripts, forms, frames, handlers
}

// Result of Sanitize.
type Result struct {
	HTML   string `json:"html"`
	Report Report `json:"report"`
}

// Elements dropped together with everything inside them.
var dropWithContent = map[atom.Atom]bool{
	atom.Script: true, atom.Noscript: true, atom.Iframe: true, atom.Frame: true, atom.Frameset: true,
	atom.Object: true, atom.Embed: true, atom.Applet: true, atom.Template: true, atom.Svg: true,
	atom.Math: true, atom.Audio: true, atom.Video: true, atom.Canvas: true, atom.Title: true,
	atom.Head: false, // head is unwrapped; its <style> is kept
}

// Elements dropped but whose children are kept.
var unwrap = map[atom.Atom]bool{
	atom.Form: true, atom.Html: true, atom.Body: true, atom.Head: true, atom.Font: false,
}

// Void/empty elements that are dropped outright.
var dropEmpty = map[atom.Atom]bool{
	atom.Link: true, atom.Meta: true, atom.Base: true, atom.Input: true, atom.Button: true,
	atom.Select: true, atom.Textarea: true, atom.Source: true, atom.Track: true, atom.Param: true,
	atom.Picture: false,
}

var allowedAttr = map[string]bool{
	"style": true, "class": true, "id": true, "dir": true, "lang": true, "title": true, "alt": true,
	"width": true, "height": true, "align": true, "valign": true, "bgcolor": true, "color": true,
	"border": true, "cellpadding": true, "cellspacing": true, "colspan": true, "rowspan": true,
	"face": true, "size": true, "role": true, "aria-label": true, "aria-hidden": true, "start": true,
	"type": true, "span": true, "nowrap": true, "scope": true, "headers": true, "abbr": true,
}

var (
	cssURL    = regexp.MustCompile(`(?i)url\s*\(\s*(['"]?)([^'")]*)(['"]?)\s*\)`)
	cssImport = regexp.MustCompile(`(?i)@import[^;]*;?`)
	cssBad    = regexp.MustCompile(`(?i)expression\s*\(|behavior\s*:|-moz-binding|javascript:`)
	cssFont   = regexp.MustCompile(`(?is)@font-face\s*\{[^}]*\}`)
)

// Sanitize cleans an HTML body.
func Sanitize(src string, o Options) Result {
	doc, err := html.Parse(strings.NewReader(src))
	var r Report
	if err != nil {
		return Result{HTML: "<pre>" + html.EscapeString(src) + "</pre>"}
	}
	var out bytes.Buffer
	s := &sanitizer{o: o, r: &r, seen: map[string]bool{}}
	for c := doc.FirstChild; c != nil; c = c.NextSibling {
		s.walk(c, &out)
	}
	return Result{HTML: Wrap(out.String(), o.AllowRemoteImages), Report: r}
}

// CSP is the content security policy every shown message carries. The app's
// web view enforces the same with its own blocking rules.
func CSP(remoteImages bool) string {
	img := "data: cid:"
	if remoteImages {
		img += " https:"
	}
	return "default-src 'none'; img-src " + img + "; style-src 'unsafe-inline'; font-src data:; form-action 'none'; base-uri 'none'"
}

// baseCSS gives every message Mail's reading defaults; the message's own
// styles come later in the document and override them.
const baseCSS = `html{-webkit-text-size-adjust:none}` +
	`body{margin:18px 22px;font:14px/1.45 -apple-system,"Helvetica Neue",sans-serif;color:#1d1d1f;word-wrap:break-word;overflow-wrap:anywhere}` +
	`img{max-width:100%;height:auto}img[data-blocked-src]{background:#f2f2f7;outline:1px dashed #c7c7cc;min-height:24px;min-width:24px}` +
	`blockquote[type=cite]{margin:4px 0 4px 2px;padding-left:10px;border-left:2px solid #1a73e8;color:#1a5fb4}` +
	`blockquote[type=cite] blockquote[type=cite]{border-left-color:#34a853;color:#2b7a3d}` +
	`a{color:#0a64d6}pre,code{font-family:ui-monospace,Menlo,monospace;font-size:12.5px}`

// Wrap puts a sanitized body in a document with the CSP and a referrer policy.
func Wrap(body string, remoteImages bool) string {
	return `<!doctype html><html><head><meta charset="utf-8">` +
		`<meta http-equiv="Content-Security-Policy" content="` + CSP(remoteImages) + `">` +
		`<meta name="referrer" content="no-referrer">` +
		`<style>` + baseCSS + `</style></head><body>` + body + `</body></html>`
}

type sanitizer struct {
	o    Options
	r    *Report
	seen map[string]bool
}

func (s *sanitizer) tracker(name string) {
	if !s.seen[name] {
		s.seen[name] = true
		s.r.Trackers = append(s.r.Trackers, name)
	}
}

func (s *sanitizer) walk(n *html.Node, w *bytes.Buffer) {
	switch n.Type {
	case html.TextNode:
		w.WriteString(html.EscapeString(n.Data))
		return
	case html.CommentNode, html.DoctypeNode:
		return
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s.walk(c, w)
		}
		return
	case html.ElementNode:
	default:
		return
	}
	if n.DataAtom == 0 { // unknown/custom element: keep the text inside
		s.children(n, w)
		return
	}
	if dropWithContent[n.DataAtom] {
		s.r.Removed++
		return
	}
	if dropEmpty[n.DataAtom] {
		if n.DataAtom != atom.Meta && n.DataAtom != atom.Link {
			s.r.Removed++
		} else if n.DataAtom == atom.Link {
			s.r.RemoteBlocked++
		}
		return
	}
	if unwrap[n.DataAtom] {
		if n.DataAtom == atom.Form {
			s.r.Removed++
		}
		s.children(n, w)
		return
	}
	if n.DataAtom == atom.Style {
		var css strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				css.WriteString(c.Data)
			}
		}
		w.WriteString("<style>")
		w.WriteString(strings.ReplaceAll(s.css(css.String()), "</", `<\/`))
		w.WriteString("</style>")
		return
	}
	if n.DataAtom == atom.Img {
		s.img(n, w)
		return
	}

	w.WriteByte('<')
	w.WriteString(n.Data)
	for _, a := range n.Attr {
		k := strings.ToLower(a.Key)
		switch {
		case a.Namespace != "":
		case k == "href" && n.DataAtom == atom.A:
			if u, ok := s.link(a.Val); ok {
				attr(w, "href", u)
				attr(w, "title", u)
			}
		case k == "background":
			s.r.RemoteBlocked++
		case k == "style":
			attr(w, "style", s.css(a.Val))
		case strings.HasPrefix(k, "on"):
			s.r.Removed++
		case allowedAttr[k]:
			attr(w, k, a.Val)
		}
	}
	if n.DataAtom == atom.A {
		w.WriteString(` target="_blank" rel="noopener noreferrer"`)
	}
	w.WriteByte('>')
	if voidElement(n.DataAtom) {
		return
	}
	s.children(n, w)
	w.WriteString("</")
	w.WriteString(n.Data)
	w.WriteByte('>')
}

func (s *sanitizer) children(n *html.Node, w *bytes.Buffer) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		s.walk(c, w)
	}
}

func voidElement(a atom.Atom) bool {
	switch a {
	case atom.Br, atom.Hr, atom.Col, atom.Wbr, atom.Area:
		return true
	}
	return false
}

func attr(w *bytes.Buffer, k, v string) {
	w.WriteByte(' ')
	w.WriteString(k)
	w.WriteString(`="`)
	w.WriteString(html.EscapeString(v))
	w.WriteByte('"')
}

// css removes every way a stylesheet can load something.
func (s *sanitizer) css(c string) string {
	if cssBad.MatchString(c) {
		s.r.Removed++
		c = cssBad.ReplaceAllString(c, "x-blocked(")
	}
	c = cssFont.ReplaceAllStringFunc(c, func(string) string { s.r.RemoteBlocked++; return "" })
	c = cssImport.ReplaceAllStringFunc(c, func(string) string { s.r.RemoteBlocked++; return "" })
	return cssURL.ReplaceAllStringFunc(c, func(m string) string {
		u := strings.TrimSpace(cssURL.FindStringSubmatch(m)[2])
		if strings.HasPrefix(strings.ToLower(u), "data:image/") || strings.HasPrefix(strings.ToLower(u), "cid:") {
			return m
		}
		s.r.RemoteBlocked++
		return "none"
	})
}

func (s *sanitizer) img(n *html.Node, w *bytes.Buffer) {
	var src, alt, style string
	var wd, ht = -1, -1
	keep := []html.Attribute{}
	for _, a := range n.Attr {
		switch strings.ToLower(a.Key) {
		case "src":
			src = strings.TrimSpace(a.Val)
		case "alt":
			alt = a.Val
			keep = append(keep, a)
		case "width":
			wd = dim(a.Val)
			keep = append(keep, a)
		case "height":
			ht = dim(a.Val)
			keep = append(keep, a)
		case "style":
			style = a.Val
		case "title", "class", "align", "border":
			keep = append(keep, a)
		}
	}
	low := strings.ToLower(src)
	remote := strings.HasPrefix(low, "http:") || strings.HasPrefix(low, "https:") || strings.HasPrefix(low, "//")
	if remote {
		if host := TrackerHost(src); host != "" {
			s.tracker(host)
			return
		}
		if (wd >= 0 && wd <= 2 && ht >= 0 && ht <= 2) || hiddenStyle(style) || (wd == 0 || ht == 0) {
			s.tracker("pixel")
			return
		}
	}
	w.WriteString("<img")
	for _, a := range keep {
		attr(w, strings.ToLower(a.Key), a.Val)
	}
	if style != "" {
		attr(w, "style", s.css(style))
	}
	switch {
	case remote && s.o.AllowRemoteImages && !strings.HasPrefix(low, "http:"):
		attr(w, "src", CleanURL(src))
	case remote && s.o.AllowRemoteImages: // upgrade http images
		attr(w, "src", "https:"+CleanURL(src)[len("http:"):])
	case remote:
		s.r.RemoteBlocked++
		attr(w, "data-blocked-src", src)
		if alt == "" {
			attr(w, "alt", "")
		}
	case strings.HasPrefix(low, "cid:"), strings.HasPrefix(low, "data:image/"):
		attr(w, "src", src)
	}
	w.WriteByte('>')
}

func dim(v string) int {
	v = strings.TrimSuffix(strings.TrimSpace(v), "px")
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}

func hiddenStyle(st string) bool {
	st = strings.ToLower(strings.ReplaceAll(st, " ", ""))
	return strings.Contains(st, "display:none") || strings.Contains(st, "visibility:hidden") ||
		strings.Contains(st, "width:1px") && strings.Contains(st, "height:1px") ||
		strings.Contains(st, "width:0") && strings.Contains(st, "height:0") || strings.Contains(st, "opacity:0;") ||
		strings.HasSuffix(st, "opacity:0")
}

// link cleans an href; it answers false for schemes that are not allowed.
func (s *sanitizer) link(href string) (string, bool) {
	href = strings.TrimSpace(href)
	u, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		c := CleanURL(href)
		if c != href {
			s.r.LinksCleaned++
		}
		return c, true
	case "mailto", "tel":
		return href, true
	case "":
		if strings.HasPrefix(href, "#") {
			return href, true
		}
	}
	return "", false
}
