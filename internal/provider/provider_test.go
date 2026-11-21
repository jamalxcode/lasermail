package provider

import "testing"

func TestDetect(t *testing.T) {
	for in, want := range map[string]Kind{
		"a@gmail.com": Gmail, "B@GoogleMail.com": Gmail, "x@icloud.com": ICloud,
		"x@me.com": ICloud, "x@mac.com": ICloud, "x@example.org": Custom, "nope": Custom,
	} {
		if got := Detect(in); got != want {
			t.Errorf("Detect(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestPresets(t *testing.T) {
	g, _ := Preset(Gmail)
	if g.IMAP.Addr() != "imap.gmail.com:993" || !g.SavesSent {
		t.Fatalf("gmail preset: %+v", g)
	}
	i, _ := Preset(ICloud)
	if i.SMTP.Addr() != "smtp.mail.me.com:587" || i.SMTP.Security != StartTLS || i.SavesSent {
		t.Fatalf("icloud preset: %+v", i)
	}
}

func TestCheckSecurity(t *testing.T) {
	ok := []Server{{"imap.gmail.com", 993, TLS}, {"127.0.0.1", 1143, Insecure}, {"localhost", 1, Insecure}, {"::1", 1, Insecure}}
	for _, s := range ok {
		if err := CheckSecurity(s); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []Server{{"imap.example.org", 143, Insecure}, {"10.0.0.1", 143, Insecure}, {"x", 1, "weird"}}
	for _, s := range bad {
		if CheckSecurity(s) == nil {
			t.Errorf("%+v: want an error", s)
		}
	}
}

func TestLoginNamesAndPasswords(t *testing.T) {
	if got := LoginNames(ICloud, " yodabasket@icloud.com "); len(got) != 2 || got[0] != "yodabasket" || got[1] != "yodabasket@icloud.com" {
		t.Fatalf("icloud names %q", got)
	}
	if got := LoginNames(Gmail, "a@gmail.com"); len(got) != 1 || got[0] != "a@gmail.com" {
		t.Fatalf("gmail names %q", got)
	}
	// Pasted from a web page: non-breaking spaces, a zero-width space, a BOM, a newline.
	if got := CleanPassword("\u00a0abcd-efgh\u200b-ijkl-mnop\ufeff\n "); got != "abcd-efgh-ijkl-mnop" {
		t.Fatalf("clean %q", got)
	}
	if got := CleanPassword("abcd efgh ijkl mnop"); got != "abcdefghijklmnop" {
		t.Fatalf("gmail groups %q", got)
	}
	if v := PasswordVariants(ICloud, "abcd-efgh-ijkl-mnop"); len(v) != 2 || v[1] != "abcdefghijklmnop" {
		t.Fatalf("variants %q", v)
	}
	if v := PasswordVariants(Gmail, "abcd-efgh-ijkl-mnop"); len(v) != 1 {
		t.Fatalf("gmail variants %q", v)
	}
}
