package privacy

import (
	"strings"
	"testing"
)

func TestSanitizeRemovesActiveContent(t *testing.T) {
	in := `<html><head><title>x</title><script>alert(1)</script><link rel=stylesheet href="https://evil/a.css">
<style>@import url(https://evil/b.css); body{background:url(https://evil/bg.png)} .a{color:red}</style></head>
<body onload="x()"><form action="https://evil/f"><input name=a>Text</form>
<iframe src="https://evil"></iframe><p onclick="x()" style="background-image:url('https://evil/p.png');color:blue">Hi</p>
<a href="javascript:alert(1)">bad</a><svg><script>1</script></svg></body></html>`
	r := Sanitize(in, Options{})
	h := r.HTML
	for _, bad := range []string{"<script", "alert", "evil", "onload", "onclick", "<iframe", "<form", "<input", "javascript:", "<svg", "@import"} {
		if strings.Contains(h, bad) {
			t.Errorf("output still contains %q:\n%s", bad, h)
		}
	}
	for _, good := range []string{"Text", "Hi", "color:blue", ".a{color:red}", "Content-Security-Policy", "default-src 'none'"} {
		if !strings.Contains(h, good) {
			t.Errorf("output lost %q:\n%s", good, h)
		}
	}
	if r.Report.RemoteBlocked < 3 {
		t.Errorf("remote blocked = %d", r.Report.RemoteBlocked)
	}
}

func TestTrackersAndPixels(t *testing.T) {
	in := `<img src="https://example.list-manage.com/track/open.php?u=1" width=1 height=1>
<img src="https://shop.example.com/p.gif" width="1" height="1">
<img src="https://cdn.example.com/hidden.png" style="display:none">
<img src="https://cdn.example.com/hero.jpg" width=600 alt="Hero">
<img src="cid:logo@x">`
	r := Sanitize(in, Options{})
	if len(r.Report.Trackers) != 2 || r.Report.Trackers[0] != "list-manage.com" || r.Report.Trackers[1] != "pixel" {
		t.Fatalf("trackers = %v", r.Report.Trackers)
	}
	if strings.Contains(r.HTML, ` src="https://cdn.example.com/hero.jpg"`) {
		t.Fatal("remote image loaded without permission")
	}
	if !strings.Contains(r.HTML, `data-blocked-src="https://cdn.example.com/hero.jpg"`) || !strings.Contains(r.HTML, `src="cid:logo@x"`) {
		t.Fatalf("image handling:\n%s", r.HTML)
	}
	// Allowed: the real image loads, the trackers still don't.
	r = Sanitize(in, Options{AllowRemoteImages: true})
	if !strings.Contains(r.HTML, ` src="https://cdn.example.com/hero.jpg"`) || strings.Contains(r.HTML, "list-manage") || strings.Contains(r.HTML, "p.gif") {
		t.Fatalf("allowed:\n%s", r.HTML)
	}
	if !strings.Contains(r.HTML, "img-src data: cid: https:") {
		t.Fatal("CSP should allow https images when remote images are allowed")
	}
}

func TestCleanURL(t *testing.T) {
	cases := map[string]string{
		"https://shop.example.com/item?id=4&utm_source=news&utm_medium=email&fbclid=abc":                        "https://shop.example.com/item?id=4",
		"https://www.google.com/url?q=https://example.com/a?gclid=1&sa=D":                                       "https://example.com/a",
		"https://eur01.safelinks.protection.outlook.com/?url=https%3A%2F%2Fexample.com%2Fx%3Fmc_eid%3D9&data=1": "https://example.com/x",
		"https://example.com/plain":   "https://example.com/plain",
		"https://example.com/?q=keep": "https://example.com/?q=keep",
	}
	for in, want := range cases {
		if got := CleanURL(in); got != want {
			t.Errorf("CleanURL(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestLinksCleanedAndSafe(t *testing.T) {
	r := Sanitize(`<a href="https://x.example/?utm_campaign=1&a=2" onmouseover="x">go</a><a href="mailto:a@b.c">m</a>`, Options{})
	if !strings.Contains(r.HTML, `href="https://x.example/?a=2"`) || !strings.Contains(r.HTML, `rel="noopener noreferrer"`) || r.Report.LinksCleaned != 1 {
		t.Fatalf("%+v\n%s", r.Report, r.HTML)
	}
	if !strings.Contains(r.HTML, `href="mailto:a@b.c"`) {
		t.Fatal("mailto lost")
	}
}

func TestTrackerHost(t *testing.T) {
	for _, u := range []string{"https://ct.sendgrid.net/wf/open?x", "https://track.example.com/a.png", "//pixel.wp.com/g.gif", "https://news.example.com/e/o/abc"} {
		if TrackerHost(u) == "" {
			t.Errorf("%s not detected", u)
		}
	}
	for _, u := range []string{"https://images.example.com/logo.png", "https://github.com/a.png"} {
		if h := TrackerHost(u); h != "" {
			t.Errorf("%s falsely detected as %s", u, h)
		}
	}
}

func FuzzSanitize(f *testing.F) {
	f.Add(`<p>hi<img src=x onerror=alert(1)></p>`)
	f.Add(`<style>a{b:url(</style>`)
	f.Fuzz(func(t *testing.T, s string) {
		h := Sanitize(s, Options{}).HTML
		if strings.Contains(strings.ToLower(h), "<script") {
			t.Fatalf("script survived: %q", h)
		}
	})
}
