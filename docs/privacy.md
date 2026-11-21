# Privacy model

**What leaves your Mac:** IMAP and SMTP traffic to your own mail providers over TLS, and (only with Sign in with
Google) the OAuth exchange with Google. Nothing else: no analytics, crash reports, update pings, avatar lookups or
link previews.

**Reading mail:** `internal/privacy` rebuilds each HTML message from an allowlist: no scripts, event handlers,
forms, frames, objects, SVG, remote stylesheets, `@import`, `@font-face` or CSS `url()` loads. Remote images
become placeholders until you choose **Load Images**. Images from known tracking services, and 1×1, zero-size or
hidden remote images, are removed even then. Links are unwrapped from redirectors and lose tracking parameters. The
page carries a CSP (`default-src 'none'`), and the WKWebView adds a second, independent block: JavaScript off,
non-persistent storage, and a content rule list that blocks every remote request (or allows only https images
after you load them).

**Sending mail:** the Date header is in UTC, the Message-ID is random on your own domain, there is no
User-Agent/X-Mailer, SMTP says `EHLO localhost`, and Bcc recipients exist only in the SMTP envelope.

**Storage:** `~/Library/Application Support/Laser Email` (0700, database 0600): message summaries, downloaded
bodies, and the search index. Secrets live only in the login Keychain. Removing an account deletes its local mail
and its Keychain items. FileVault protects the folder at rest. App-level encryption of the database is a possible
future step (it would also mean encrypting the search index).
