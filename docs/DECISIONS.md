# Decisions and thought process

Why Laser Email is built the way it is. Each entry: what was decided, what else was considered, and why.
`BUSINESS-RULES.md` lists the resulting rules.

## The brief

"An email client for macOS in Go, focused on privacy, handling many emails from different Gmail and Apple
accounts, called Laser Email", then "follow the style and GUI of an Apple macOS email client".

Read as four requirements: native-feeling Mac app, Go, many Gmail + iCloud accounts at once, privacy first.

## 1. Architecture: Go engine + SwiftUI shell

- **Decision:** all logic in a pure-Go daemon (`laserd`); a thin SwiftUI app talks to it over a Unix socket.
- **Considered:** an all-Go GUI (Fyne, Gio), Go + web UI (Wails), Go driving AppKit through cgo or purego.
- **Why:** "the style and GUI of an Apple macOS email client" means real AppKit/SwiftUI controls: the sidebar,
  toolbar, split view, menus, swipe actions and system behaviours users expect. Fyne/Gio draw their own widgets and
  never look native; Wails is a web page in a window. The same split already works in `laserdot-go` (Go core +
  SwiftUI shell), so it is the house pattern. Everything that matters (sync, storage, privacy, sending) is Go
  and testable on Linux; the Swift side has no logic worth unit-testing beyond decoding.
- **Cost:** two languages; Swift compiles only on macOS (handled in CI).

## 2. IMAP/SMTP for both Gmail and iCloud

- **Decision:** one protocol path (IMAP + SMTP) for every provider.
- **Considered:** the Gmail REST API.
- **Why:** iCloud only offers IMAP, so IMAP is needed anyway; one code path is less to get wrong. The Gmail API
  would add HTTP to the engine and still need Google's verification. Gmail quirks are handled as rules: All Mail
  isn't synced (S3), archive = leave the inbox (X1), Gmail files sent mail itself (W6).

## 3. App passwords first, Google OAuth optional

- **Decision:** accounts sign in with app passwords; "Sign in with Google" ships only when a Google OAuth client
  is configured.
- **Why:** Apple has no OAuth for third-party IMAP; app-specific passwords are its only way. Google supports app
  passwords (with 2-Step Verification). The `https://mail.google.com/` scope is "restricted": a public app needs
  Google's verification and a security assessment, which is out of reach at first. OAuth is fully built (PKCE,
  loopback redirect, refresh, XOAUTH2) so it switches on with a client id.

## 4. Privacy model: defense in depth

- **Decision:** two independent layers decide what a message may load: the Go sanitizer (allowlist rebuild,
  tracker removal, link cleaning, CSP) and WebKit (JavaScript off, non-persistent store, content rule lists).
- **Why:** either layer alone could have a bug; together a mistake in one is caught by the other. Rule lists are
  compiled once and installed before any HTML is loaded (a mail is never shown without them).
- **Remote images:** blocked by default with per-message and per-sender opt-in, like Mail's "Load Remote Content".
  Apple's Mail Privacy Protection fetches images through a relay; we have no server and won't run one (it would
  see users' mail), so blocking is the honest option. Tracking pixels stay blocked even after opting in.
- **Outgoing mail:** UTC dates hide the time zone, random Message-IDs hide the host and app, EHLO `localhost` hides
  the computer name, no User-Agent. Read receipts are never sent.
- **No network outside three packages,** enforced by a test (`internal/netcheck`), not by convention.

## 5. Local storage

- **Decision:** SQLite through `modernc.org/sqlite` (pure Go, no cgo), FTS5 for search, raw messages stored as
  blobs, secrets in the Keychain.
- **Why:** pure Go keeps cross-compiling and CI simple (as in laserdot-go). FTS5 gives fast local search across
  accounts without sending queries to providers.
- **Not yet:** app-level encryption of the database. FileVault encrypts it at rest; encrypting it ourselves would
  also mean encrypting the search index, which needs a different design. Recorded as future work.

## 6. Sync strategy

- **Decision:** windowed, incremental sync. Newest 2,000 summaries and 300 bodies per folder at first; UID-based
  increments; flags and deletions reconciled each pass; IDLE on the inbox; a full pass every 10 minutes.
- **Why:** people with many accounts often have huge mailboxes; downloading everything first would take hours and
  gigabytes. Recent mail is what people read and search. Two connections per account keep actions responsive
  while IDLE waits.
- **Bug found by CI (fixed):** a sync request arriving as IDLE woke up could be lost for 10 minutes. The worker now
  waits for its IDLE watcher before the next pass (S11).

## 7. Native mail look

- **Decision:** follow macOS Mail's conventions (three columns, Favorites + per-account sections, row layout,
  toolbar, shortcuts, the provider-choice sheet) with standard SwiftUI controls and SF Symbols.
- **Not copied:** Apple's or Google's artwork and logos. The app icon is original (an envelope cut by a laser line).

## 8. Testing approach

- **Engine:** end-to-end tests against go-imap's in-memory IMAP server and a small fake SMTP server cover two
  accounts at once: unified inbox, search, privacy, flags both ways, IDLE push, archive/trash/delete, send with
  and without a Sent copy, reply/forward, attachments, removal, restart. Fuzz tests for the parser and sanitizer.
- **App:** CI builds and tests the Swift package on a real Mac, then a self-test drives the actual app against
  `laserd --demo` and prints screenshots into the log, so the UI can be checked without a Mac at hand. The
  screenshots caught real bugs (blank first HTML message, double-spaced plain text, a mis-encoded subject).
- **Not yet:** a run against real Gmail and iCloud accounts (needs credentials).

## 9. Distribution

- **Decision:** a GitHub release with a universal DMG, published automatically from `main` when the changelog has a
  new version, after a launch check of the app inside the DMG.
- **Signing:** ad-hoc only. Developer ID signing and notarization need a paid Apple Developer account; until then
  users open it once via System Settings → Privacy & Security → Open Anyway. Notarization is the next step for a
  smooth install.
- **No auto-updater yet:** an updater would be the one networked part of the app; it is left out until there is a
  signed release channel to check against (laserdot-go's updater, with build attestations, is the model).

## 10. Repository

- First created as `jamalxcode/laser-email`, then moved to **`jamalxcode/lasermail`** at the owner's request;
  the Go module path follows the repo.
- The repo started private and was made **public** so the DMG downloads without a GitHub login (and macOS CI
  minutes are free). The old `laser-email` repo was deleted.
