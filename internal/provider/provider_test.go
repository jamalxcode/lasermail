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
