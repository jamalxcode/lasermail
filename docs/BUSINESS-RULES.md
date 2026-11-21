# Business rules

Every rule Laser Email follows, and where it lives in the code. Keep this file in step with the code: a change in
behaviour changes this file in the same commit. The reasoning behind the rules is in `DECISIONS.md`.

## 1. Accounts

| # | Rule | Where |
|---|---|---|
| A1 | Supported kinds: **Gmail** (incl. Google Workspace), **iCloud** (icloud.com, me.com, mac.com) and **any IMAP/SMTP server**. | `internal/provider` |
| A2 | The kind is guessed from the address: gmail.com/googlemail.com → Gmail; icloud.com/me.com/mac.com → iCloud; anything else → the user picks (Workspace domains can't be told apart from the address). | `provider.Detect` |
| A3 | Presets: Gmail `imap.gmail.com:993` TLS, `smtp.gmail.com:465` TLS. iCloud `imap.mail.me.com:993` TLS, `smtp.mail.me.com:587` STARTTLS. | `provider.presets` |
| A4 | Sign-in is an **app password** (Gmail: app password with 2-Step Verification; iCloud: app-specific password) or, for Gmail only and only in builds with a Google OAuth client, **Sign in with Google** (XOAUTH2). | `engine.AddAccount` |
| A5 | A typed or pasted password is cleaned: every kind of space (Google shows app passwords in groups of four), line breaks and invisible characters (zero-width spaces, BOMs, direction marks) are removed. | `provider.CleanPassword` |
| A6 | An account is saved **only after a successful IMAP login**. A refused login says what to check for that provider (iCloud: an app-specific password for that Apple Account; Gmail: an app password with 2-Step Verification). | `engine.tryLogin`, `imapsync.ErrAuth` |
| A6a | **iCloud** IMAP logs in with the name part of the address first (`yodabasket`), then the full address, as Apple's settings say. An app-specific password shown as `xxxx-xxxx-xxxx-xxxx` is tried as typed, then without hyphens. The user name that worked is saved with the account; the password form that worked goes to the Keychain. Only a refused login moves on to the next try. | `provider.LoginNames`, `provider.PasswordVariants`, `engine.tryLogin` |
| A6b | SMTP always signs in with the full address (iCloud requires it there). | `engine.Send` |
| A6c | In the Add Account sheet, **Back** returns to the provider list with a clean form (name back to the Mac user's name; address, password and servers empty). | `AddAccountSheet.resetForm` |
| A7 | One account per address: adding the same address again (any letter case) is refused. | `store.AddAccount` (UNIQUE, lower-cased) |
| A8 | The account label defaults to "Gmail", "iCloud", or the IMAP host. | `engine.AddAccount` |
| A9 | The password or OAuth token goes to the **Keychain** (service "Laser Email", account `account-<id>-password` / `account-<id>-oauth`), never to the database. If the Keychain write fails, the account is not added. | `internal/secret`, `engine.AddAccount` |
| A10 | Removing an account stops its sync, deletes its Keychain items and **every local copy** of its mail. Nothing changes on the server. | `engine.RemoveAccount` |
| A11 | A revoked app password can be replaced in Settings; the new one is checked with a login before it is saved. | `engine.UpdatePassword` |
| A12 | OAuth access tokens are refreshed when they have less than a minute left; a failed refresh asks the user to sign in again. | `engine.credentials` |

## 2. Connections

| # | Rule | Where |
|---|---|---|
| C1 | **Encrypted only**: implicit TLS or STARTTLS, TLS 1.2 or newer, certificate checked against the host name. Plain text is refused except to a loopback address (tests, local bridges). | `provider.CheckSecurity`, `imapsync.Dial`, `send.Send` |
| C2 | If a STARTTLS server does not offer STARTTLS, sending is refused rather than done in plain text. | `send.Send` |
| C3 | No IMAP `ID` command is sent (it would identify the client). | `imapsync.Dial` |
| C4 | Each account has two connections: one for sync and IDLE, one for the user's actions (so a click never waits for IDLE). The action connection reconnects once if it dropped. | `engine/worker.go` |

## 3. Sync

| # | Rule | Where |
|---|---|---|
| S1 | Folder roles come from IMAP SPECIAL-USE (`\Sent`, `\Drafts`, `\Trash`, `\Junk`, `\Archive`, `\All`); without it, from well-known names (Sent Messages, Deleted Messages, Spam, …). `INBOX` is always the inbox. | `imapsync.roleOf` |
| S2 | Folders marked `\Noselect` or `\NonExistent` are skipped; folders that disappear from the server are forgotten locally with their mail. | `imapsync.SyncFolders` |
| S3 | **Gmail's All Mail is not synced**: it repeats every other folder. Archive still uses it on the server. | `engine.syncedRole` |
| S4 | A folder's first sync downloads summaries of its **newest 2,000** messages (500 for Trash, Junk and custom folders). Older mail stays on the server. | `imapsync.DefaultOptions`, `worker.syncBox` |
| S5 | Full bodies of the **newest 300** messages per folder (30 for Trash/Junk/custom) are downloaded during sync, if they are 2 MB or smaller, so they are searchable and preview instantly. Other bodies download when opened. | `imapsync.prefetch` |
| S6 | Bodies are fetched with `BODY.PEEK`, so syncing never marks mail as read. | `imapsync.fullSection` |
| S7 | If a folder's UIDVALIDITY changes, its local copy is discarded and downloaded again. | `imapsync.SyncMailbox` |
| S8 | Each pass reconciles flags (read, flagged, answered) and deletions for every message held locally, then fetches messages above the last UID seen. | `imapsync.SyncMailbox` |
| S9 | The inbox syncs first, then the other folders. | `worker.syncAll` |
| S10 | Between passes the sync connection **IDLEs on the inbox**: new mail appears as soon as the server reports it. IDLE is renewed every 25 minutes. Servers without IDLE are checked every 2 minutes. | `imapsync.Idle`, `worker.session` |
| S11 | Every folder is re-checked every 10 minutes, after an action that moves mail, and on **Get Mail** (⇧⌘N). A sync request is never lost, even if it arrives as the server wakes IDLE. | `worker.session` |
| S12 | After a connection error the account shows the error and retries after 5 s, doubling up to 5 minutes; Get Mail retries at once. | `worker.run` |
| S13 | A "new mail" event (notification, refresh) is raised only for mail arriving in an inbox **after** its first sync, never for the initial download. | `worker.syncBox` |
| S14 | Threads are grouped by the first `References` id, else `In-Reply-To`, else the message's own id. The same message in two folders (Gmail labels) is shown once in a thread. | `store.ThreadKey`, `store.Thread` |

## 4. Views and lists

| # | Rule | Where |
|---|---|---|
| V1 | **All Inboxes** lists every account's inbox together, newest first. Favorites also offer Flagged, Drafts and Sent across accounts. | `store.List` (Role) |
| V2 | **Flagged** lists flagged mail in every folder except Trash and Gmail's All Mail; its badge counts the same. | `store.List`, `store.Counts` |
| V3 | Badges: All Inboxes = unread across inboxes; each account's Inbox, Junk and custom folders show their unread count; Sent, Drafts, Trash and Archive show none. The Dock badge = unread across inboxes. | `SidebarView`, `AppModel.refreshSidebar` |
| V4 | In unified views each row names its account (when there is more than one account). | `MessageListView.showAccount` |
| V5 | Lists show at most 200 messages per request, newest first; **Show Only Unread** filters any view. | `store.List`, `AppModel.query` |
| V6 | Dates in the list: time today, "Yesterday", weekday within a week, else the short date. | `LaserKit.listDate` |

## 5. Search

| # | Rule | Where |
|---|---|---|
| Q1 | Search runs **locally** over subject, sender and body text (downloaded bodies; otherwise the preview). | `store.List`, FTS5 table `search` |
| Q2 | Every word must match, as a word prefix; accents and case are ignored. Quotes and search operators typed by the user are stripped, so no input can break the query. | `store.ftsQuery` |
| Q3 | Search covers every account and folder except Trash, Junk and Gmail's All Mail. | `store.List` |

## 6. Reading and privacy

| # | Rule | Where |
|---|---|---|
| P1 | Every HTML message is rebuilt from an allowlist before it is shown: scripts, event handlers, forms, inputs, frames, objects, embeds, SVG, MathML, audio/video, `<link>`, `<meta>`, `<base>` are removed; CSS loses `@import`, `@font-face`, `url()` loads (except `data:image` and `cid:`), `expression()`, `behavior:` and `-moz-binding`. | `privacy.Sanitize` |
| P2 | **Remote images are not loaded** until the user clicks **Load Images** (this message) or **Always Load from This Sender**. Blocked images keep their size as a placeholder. | `privacy.img`, `engine.Open`, `store.senders` |
| P3 | **Tracking pixels are always blocked**, even when images are allowed: images from 100+ known tracking services and open-tracking paths, and remote images that are 1×1/2×2, zero-size or hidden by CSS. | `privacy.TrackerHost`, `privacy.img` |
| P4 | Links are unwrapped from redirectors (Google, Outlook Safe Links, Facebook, …) and lose tracking parameters (`utm_*`, `fbclid`, `gclid`, `mc_eid`, HubSpot, Marketo, …). Only http(s), mailto and tel links are kept; they open in the default browser with no referrer. | `privacy.CleanURL`, `MailWebView` |
| P5 | `http` images are upgraded to `https` when images are allowed. | `privacy.img` |
| P6 | The page carries a Content Security Policy (`default-src 'none'`; images `data:`/`cid:` and, if allowed, `https:`), and `no-referrer`. | `privacy.Wrap`, `privacy.CSP` |
| P7 | The viewer (WKWebView) has **JavaScript off**, a non-persistent data store (no cookies/cache), no link previews, and a content rule list that blocks every remote request (or allows only https images after Load Images). Mail is never shown before the rules are installed. | `MailWebView`, `BlockRules` |
| P8 | Inline (`cid:`) images are served from the downloaded message, never from the network. | `MailWebView.Coordinator`, `engine.InlinePart` |
| P9 | A banner says what was blocked ("Laser blocked 2 trackers, hid 1 remote image and cleaned 1 link"). | `LaserKit.privacySummary`, `PrivacyBanner` |
| P10 | **Read receipts are never sent.** If the sender asked for one, the reader says so. | `msg.Parse` (WantsReadReceipt), `ReaderView` |
| P11 | Opening a message marks it read on the server, then locally. Mark as Unread undoes it. | `engine.Open`, `engine.SetFlag` |
| P12 | Avatars are initials on a color derived from the address; no picture is ever fetched. | `Avatar`, `LaserKit.initials` |
| P13 | Plain-text mail is escaped, URLs are linked, `>` quotes become quote bars. | `msg.TextToHTML` |

## 7. Actions

| # | Rule | Where |
|---|---|---|
| X1 | **Archive** moves to the account's Archive folder; on Gmail (no Archive) to All Mail, which removes it from the inbox. | `engine.MoveToRole` |
| X2 | **Delete** moves to the account's Trash; deleting **in Trash** removes the message for good. | `engine.Delete` |
| X3 | **Junk** moves to the account's Junk/Spam folder. | `engine.MoveToRole` |
| X4 | Messages move only within their own account. | `engine.MoveTo` |
| X5 | An action changes the server first; only then is the local copy updated. The moved message reappears in its new folder on the next sync, which starts at once. | `engine.MoveTo` |
| X6 | After a message leaves the list, the next one down is selected (like Mail). | `AppModel.neighbour` |
| X7 | Attachments are saved to **Downloads** (and shown in Finder). A file is never overwritten ("name 2.pdf"); names are stripped of folders and leading dots. | `engine.SaveAttachment` |

## 8. Writing and sending

| # | Rule | Where |
|---|---|---|
| W1 | A message needs at least one recipient (To, Cc or Bcc). | `engine.Send` |
| W2 | From is the account's address with the user's name. A new message starts from the account being viewed, else the first account; replies use the account that received the message. | `engine.Send`, `ComposeView.defaultAccount` |
| W3 | **Reply** goes to Reply-To if present, else From. **Reply All** adds To and Cc, without yourself or duplicates. Subjects get "Re: "/"Fwd: " only once. | `engine.Reply`, `msg.ReplySubject` |
| W4 | Replies quote the original under "On <date>, <name> <address> wrote:" and set In-Reply-To/References. **Forward** includes the attachments. | `engine.Reply`, `engine.Forward` |
| W5 | Outgoing mail is quiet: **Date in UTC** (to the minute), a **random Message-ID** on the sender's domain, **no User-Agent/X-Mailer**, SMTP greets as `localhost`. **Bcc** exists only in the SMTP envelope. | `msg.Build`, `send.Send` |
| W6 | A copy goes to the Sent folder unless the server files it itself (Gmail does). If saving the copy fails, the message is still sent and the failure is logged. | `engine.Send`, `provider.Settings.SavesSent` |
| W7 | After a reply is sent, the original is marked answered. | `engine.markAnswered` |

## 9. Data and privacy guarantees

| # | Rule | Where |
|---|---|---|
| D1 | Data lives in `~/Library/Application Support/Laser Email` (0700); the database and socket are 0600. | `store.Open`, `ipc.Listen`, `cmd/laserd` |
| D2 | The only network traffic is to the user's IMAP/SMTP servers, for Sign in with Google Google's OAuth endpoints, and, when the user chooses Check for Updates…, GitHub. No analytics, crash reports, automatic update checks, avatars or link previews. | `internal/netcheck` |
| D3 | Only `internal/imapsync`, `internal/send` and `internal/oauth` may use network packages; `internal/ipc` uses Unix sockets only; no dependency may speak HTTP; the Swift app may not use URLSession/Network (except the updater helper, which may name GitHub hosts only) and must install WebKit block rules. The build fails otherwise. | `internal/netcheck` |
| D4 | Passwords go to the Keychain through `/usr/bin/security -i` with the value on standard input, never on the command line. | `secret.Keychain` |

## 10. The app and the engine

| # | Rule | Where |
|---|---|---|
| E1 | The app finds the engine's socket; if nothing answers, it starts the bundled `laserd`. It quits the engine it started when it quits (⌘Q or SIGTERM); an engine it found running is left alone. | `DaemonLauncher`, `AppDelegate` |
| E2 | One engine per data folder: a second `laserd` refuses to start. | `ipc.Listen` |
| E3 | Closing the last window keeps the app running (like Mail). | `AppDelegate` |
| E4 | New mail raises a local notification with the newest unread message's sender, subject and preview. | `AppModel.notifyNewMail` |
| E5 | With no accounts, the Add Account sheet opens on launch. | `AppModel.start` |
| E6 | `laserd --demo` (and `LASER_DEMO=1` for the app) shows two made-up accounts with sample mail, offline. | `engine.SeedDemo` |

## 11. Updates

| # | Rule | Where |
|---|---|---|
| U1 | **Laser Email ▸ Check for Updates…** checks for a new version. There is no background or scheduled check. | `UpdateController`, `MailCommands` |
| U2 | The check is done by the bundled **Laser Email Updater** helper, the only Swift code with network access. It reaches only `github.com` and GitHub's release-download hosts, over HTTPS, with no cookies or cache, sends no credentials or Mac details, and refuses redirects elsewhere. | `LaserEmailUpdater/main.swift`, `internal/netcheck` |
| U3 | The newest version is read from where `github.com/jamalxcode/lasermail/releases/latest` redirects; if it is newer than the running app, `LaserEmail-<v>.dmg` and its proof `LaserEmail-<v>.dmg.sigstore.json` are downloaded (afresh each time) to `~/Library/Application Support/Laser Email/Updates`. | `LaserEmailUpdater` |
| U4 | **Nothing is installed without proof** that this repository's release workflow built the disk image (a GitHub build attestation, checked offline against Sigstore's root). Then: it holds an app with Laser Email's bundle id, with `laserd`, and a newer version. Releases before 0.1.4 have no proof and can't be installed this way. | `ReleaseProof`, `Updater.prepare` |
| U5 | The user sees the new version and what's new and chooses **Install and Relaunch** or **Later**. | `UpdateController` |
| U6 | Installing copies the new app beside the current one, moves the current one to `~/Library/Application Support/Laser Email Previous Version`, puts the new one in its place (restoring the old one on any failure), quits (stopping the engine it started) and relaunches. Mail, accounts and Keychain items are untouched. | `Updater.install` |
| U7 | An app that macOS runs from a temporary copy (not moved to Applications) is asked to be moved first. | `UpdateError.translocated` |

## 12. Releases

| # | Rule | Where |
|---|---|---|
| R1 | A release is published when CI passes on `main` and the newest version in `CHANGELOG.md` has no release yet (or on a `v*` tag, or by hand). | `.github/workflows/release.yml` |
| R2 | The disk image holds a universal (Apple silicon + Intel) app for macOS 14+, ad-hoc signed. It is published only after the app in it started and launched its bundled engine, and the engine stopped when the app quit. A SHA-256 checksum is published with it. | `release.yml` |
| R3 | Each disk image gets a GitHub build attestation; the release is published only after the app's own proof check accepts it, and the proof is published beside the image. After publishing, the updater helper is run against the live release and the app's checks must accept what it downloaded. | `release.yml` |
