// Package netcheck holds the test that keeps network code where it belongs.
// Laser Email talks to exactly three kinds of hosts: the user's IMAP and
// SMTP servers and, for "Sign in with Google", Google's OAuth endpoints.
// Everything else (the store, the privacy filter, the parser, the app shell)
// must not be able to reach the network at all.
package netcheck
