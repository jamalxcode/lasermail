// Package provider holds the server settings for the account kinds Laser
// Email supports: Gmail (incl. Google Workspace), iCloud Mail and any other
// IMAP/SMTP server.
package provider

import (
	"fmt"
	"net"
	"strings"
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

// LoginName is the IMAP/SMTP user name. iCloud accepts the full address; so
// does Gmail.
func LoginName(k Kind, email string) string { return strings.TrimSpace(email) }

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
