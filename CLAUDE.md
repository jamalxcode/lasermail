# CLAUDE.md

Guidance for Claude Code in this repository.

## Project

**Laser Email**: a private email client for macOS that handles many Gmail and iCloud accounts (and any other IMAP
server) in one unified inbox. Built like `jamalxcode/laserdot-go`: a pure-Go engine (`laserd`) does everything
(accounts, IMAP sync with IDLE push, the local store and search, the privacy filter, sending), and a thin SwiftUI
shell in the style of a native macOS mail app talks to it over a Unix socket.

## Pillars (don't break these)

1. **Privacy by construction.** Mail is shown only after `internal/privacy` sanitizes it (no scripts, forms,
   frames, remote fonts/styles; remote images blocked until the user asks; tracking pixels always blocked; links
   lose tracking parameters and redirect wrappers) *and* inside a WKWebView with JavaScript off, a non-persistent
   data store and a `WKContentRuleList` that blocks remote loads. Outgoing mail has a UTC date, a random
   Message-ID, no User-Agent/X-Mailer; SMTP greets as `localhost`. Read receipts are never sent. Avatars are
   initials, never fetched.
2. **Network only where it must be.** Only `internal/imapsync` (IMAP), `internal/send` (SMTP) and `internal/oauth`
   (Google sign-in) talk to the network; `internal/ipc` uses Unix sockets only. `internal/netcheck` fails the
   build otherwise, and also if a dependency speaks HTTP or the Swift app uses URLSession/Network. No telemetry.
3. **Secrets in the Keychain.** Passwords and OAuth tokens go through `internal/secret` (macOS Keychain via
   `/usr/bin/security -i`, values on stdin, never argv). The database never holds a secret.
4. **Local data stays local** in `~/Library/Application Support/Laser Email` (0700; db 0600).
5. **Encrypted connections only.** `provider.CheckSecurity` refuses plain text except to loopback (tests, bridges).

## Layout

```
internal/provider  Gmail/iCloud presets, detection, connection-security rule
internal/secret    Keychain (darwin) / 0600 file (elsewhere) / memory (tests)
internal/oauth     Google OAuth for installed apps (PKCE + loopback), XOAUTH2
internal/msg       MIME parsing (charsets, attachments), plain text → HTML, building outgoing mail
internal/privacy   HTML sanitizer, tracker list, link cleaning, CSP
internal/store     SQLite (modernc, pure Go): accounts, mailboxes, messages, bodies, FTS5 search
internal/imapsync  IMAP connection, folder/role discovery, incremental sync (UIDVALIDITY/UIDNEXT), actions, IDLE
internal/send      SMTP (TLS/STARTTLS, PLAIN/XOAUTH2) + a fake server for tests
internal/engine    the core: accounts, one sync worker per account, actions, compose, demo data
internal/ipc       JSON lines over a 0600 Unix socket
internal/daemon    protocol ops → engine
internal/netcheck  the network-boundary tests
cmd/laserd         the engine daemon (--demo for offline sample data)
cmd/laser          CLI client
app/               SwiftUI app (SwiftPM): LaserKit = socket client + wire types; LaserEmail = the UI
app/scripts        build-go.sh, bundle.sh, make-dmg.sh, selftest.sh (screenshots into the CI log)
```

## Commands

```bash
go build ./... && go vet ./... && gofmt -l . && go test -race ./...
go run ./cmd/laserd --demo --data /tmp/lsr --socket /tmp/lsr.sock   # offline demo engine
LASER_SOCKET=/tmp/lsr.sock go run ./cmd/laser ls
# macOS only:
app/scripts/build-go.sh && (cd app && swift build && LASERD=$PWD/build/go/laserd swift test)
app/scripts/bundle.sh && app/scripts/selftest.sh
```

## Testing

- `internal/engine/engine_test.go` runs two accounts end to end against go-imap's in-memory IMAP server and the
  fake SMTP server: unified inbox, search, privacy, flags both ways, IDLE push, archive/trash/delete, send with
  and without a Sent copy, reply/forward, attachments, account removal, restart.
- Swift compiles only on macOS: the CI `app` job builds, tests (against a real `laserd --demo`), bundles, and runs
  `SelfTest.swift`, printing screenshots as base64 JPEG between `=== SCREENSHOT name` lines. Decode them to look
  at the UI; extend the self-test for visible changes.
- Run gofmt, vet and the full suite before committing.

## Conventions

- Wire types: Go JSON is snake_case; Swift decodes with `.convertFromSnakeCase` and treats null arrays as empty.
  Changing a JSON shape means changing `app/Sources/LaserKit/Models.swift` too (and bumping `ipc.Version` if
  incompatible).
- The UI follows macOS mail conventions (three columns, Favorites/accounts sidebar, Mail's shortcuts) with
  standard SwiftUI controls and SF Symbols. No third-party brand logos.
