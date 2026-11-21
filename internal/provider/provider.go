// Package provider holds the server settings for the account kinds Laser
// Email supports: Gmail (incl. Google Workspace), iCloud Mail and any other
// IMAP/SMTP server.
package provider

import (
	"fmt"
	"net"
	"strings"
	"unicode"
)

// Kind of account.
type Kind string

const (
	Gmail  Kind = "gmail"
	ICloud Kind = "icloud"
	Custom Kind = "imap"
)

// Security of a connection.
type Security string

const (
	TLS      Security = "tls"      // implicit TLS (993, 465)
	StartTLS Security = "starttls" // plain connection upgraded with STARTTLS (587)
	// Insecure is plain text. It is refused for anything but a loopback
	// address (tests and local bridges such as Proton Bridge).
	Insecure Security = "insecure"
)

// Auth method.
type Auth string

const (
	Password Auth = "password" // app password (Gmail with 2-step) or app-specific password (iCloud)
	OAuth    Auth = "oauth"    // Google OAuth 2.0 (XOAUTH2)
)

// Server is one endpoint.
type Server struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Security Security `json:"security"`
}

// Addr is host:port.
func (s Server) Addr() string { return net.JoinHostPort(s.Host, fmt.Sprint(s.Port)) }

// Settings for an account kind.
type Settings struct {
	Kind Kind   `json:"kind"`
	IMAP Server `json:"imap"`
	SMTP Server `json:"smtp"`
	// SavesSent: the server files sent mail by itself (Gmail does), so the
	// client must not APPEND a copy to Sent.
	SavesSent bool `json:"saves_sent"`
	// HelpURL tells the user where to make an app password.
	HelpURL string `json:"help_url"`
	// IMAPUser is the user name the server accepted when the account was
	// added (see LoginNames); empty means the first candidate.
	IMAPUser string `json:"imap_user,omitempty"`
}

var presets = map[Kind]Settings{
	Gmail: {
		Kind:      Gmail,
		IMAP:      Server{"imap.gmail.com", 993, TLS},
		SMTP:      Server{"smtp.gmail.com", 465, TLS},
		SavesSent: true,
		HelpURL:   "https://myaccount.google.com/apppasswords",
	},
	ICloud: {
		Kind:    ICloud,
		IMAP:    Server{"imap.mail.me.com", 993, TLS},
		SMTP:    Server{"smtp.mail.me.com", 587, StartTLS},
		HelpURL: "https://account.apple.com/account/manage",
	},
}

// Preset returns the settings for a known kind.
func Preset(k Kind) (Settings, bool) {
	s, ok := presets[k]
	return s, ok
}

var icloudDomains = map[string]bool{"icloud.com": true, "me.com": true, "mac.com": true}

// Detect guesses the kind from an address. Google Workspace domains can't be
// told apart from the address alone; the user picks Gmail for those.
func Detect(email string) Kind {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return Custom
	}
	d := strings.ToLower(strings.TrimSpace(email[at+1:]))
	switch {
	case d == "gmail.com" || d == "googlemail.com":
		return Gmail
	case icloudDomains[d]:
		return ICloud
	}
	return Custom
}

// LoginNames are the IMAP user names to try, in order. Apple's settings for
// iCloud Mail give the name part of the address ("emilyparker", not
// "emilyparker@icloud.com") and say to try the full address if that fails.
// Gmail and other servers take the full address.
func LoginNames(k Kind, email string) []string {
	email = strings.TrimSpace(email)
	if k == ICloud {
		if at := strings.LastIndexByte(email, '@'); at > 0 {
			return []string{email[:at], email}
		}
	}
	return []string{email}
}

// LoginName is the first user name to try (see LoginNames).
func LoginName(k Kind, email string) string { return LoginNames(k, email)[0] }

// CleanPassword removes what copying a password from a web page or a Notes
// window drags along: spaces of every kind (Google shows app passwords in
// groups of four), line breaks, and invisible characters such as zero-width
// spaces, BOMs and direction marks.
func CleanPassword(p string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, p)
}

// LooksLikeAppPassword: Apple's app-specific passwords and Google's app
// passwords are 16 lowercase letters (shown as xxxx-xxxx-xxxx-xxxx or in four
// groups of four). Anything else is probably the account's real password.
func LooksLikeAppPassword(p string) bool {
	p = strings.ReplaceAll(CleanPassword(p), "-", "")
	if len(p) != 16 {
		return false
	}
	for _, r := range p {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// PasswordVariants are the forms of a cleaned password to try. Apple shows
// app-specific passwords as xxxx-xxxx-xxxx-xxxx; if the hyphenated form is
// refused, the same letters without hyphens are tried.
func PasswordVariants(k Kind, p string) []string {
	out := []string{p}
	if k == ICloud && strings.Count(p, "-") == 3 && len(p) == 19 {
		out = append(out, strings.ReplaceAll(p, "-", ""))
	}
	return out
}

// CheckSecurity refuses plain-text connections to anything but loopback.
func CheckSecurity(s Server) error {
	switch s.Security {
	case TLS, StartTLS:
		return nil
	case Insecure:
		if s.Host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(s.Host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("refusing an unencrypted connection to %s: only loopback servers may skip TLS", s.Host)
	}
	return fmt.Errorf("unknown security %q", s.Security)
}
