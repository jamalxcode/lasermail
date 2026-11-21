package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAuthorizeAndRefresh(t *testing.T) {
	var gotVerifier string
	tok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "the-code" || r.Form.Get("client_id") != "cid" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "bad"})
				return
			}
			gotVerifier = r.Form.Get("code_verifier")
			json.NewEncoder(w).Encode(map[string]any{"access_token": "a1", "refresh_token": "r1", "expires_in": 3600})
		case "refresh_token":
			json.NewEncoder(w).Encode(map[string]any{"access_token": "a2", "expires_in": 3600})
		}
	}))
	defer tok.Close()
	c := Config{ClientID: "cid", AuthURL: "https://consent.example/auth", TokenURL: tok.URL}
	open := func(u string) error {
		p, _ := url.Parse(u)
		q := p.Query()
		if q.Get("scope") != GmailScope || q.Get("code_challenge_method") != "S256" || q.Get("login_hint") != "me@gmail.com" {
			t.Errorf("auth url %s", u)
		}
		// Play the browser: Google redirects back to the loopback listener.
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + url.QueryEscape(q.Get("state")))
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	tk, err := c.Authorize(context.Background(), "me@gmail.com", open)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Access != "a1" || tk.Refresh != "r1" || !tk.Valid() || len(gotVerifier) < 43 {
		t.Fatalf("token %+v verifier %q", tk, gotVerifier)
	}
	nt, err := c.Refresh(context.Background(), tk)
	if err != nil || nt.Access != "a2" || nt.Refresh != "r1" {
		t.Fatalf("refresh %+v %v", nt, err)
	}
}

func TestStateMismatch(t *testing.T) {
	c := Config{ClientID: "cid", AuthURL: "https://x/auth", TokenURL: "http://127.0.0.1:1/"}
	_, err := c.Authorize(context.Background(), "", func(u string) error {
		p, _ := url.Parse(u)
		go func() {
			resp, err := http.Get(p.Query().Get("redirect_uri") + "?code=c&state=forged")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	})
	if err == nil {
		t.Fatal("forged state accepted")
	}
}

func TestXOAuth2(t *testing.T) {
	mech, ir, _ := XOAuth2{"me@gmail.com", "tok"}.Start()
	if mech != "XOAUTH2" || string(ir) != "user=me@gmail.com\x01auth=Bearer tok\x01\x01" {
		t.Fatalf("%s %q", mech, ir)
	}
}
