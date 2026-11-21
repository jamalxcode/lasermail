# Changelog

## 0.1.5

- A release for trying **Check for Updates…**: from 0.1.4, choose Laser Email ▸ Check for Updates…, then Install
  and Relaunch. Laser Email ▸ About Laser Email then shows 0.1.5. Nothing else changed.

## 0.1.4

- **Laser Email ▸ Check for Updates…** downloads the newest version from GitHub and, after you agree, installs it
  and relaunches. Each update is checked first: it must carry GitHub's proof that Laser Email's own release
  workflow built it. The previous version is kept in ~/Library/Application Support/Laser Email Previous Version.
- Updates are checked only when you choose the command, never in the background.

## 0.1.3

- Add Account: going Back to the provider list clears the form, so an iCloud entry no longer shows up when you
  pick Gmail.
- Gmail: when Google answers "Application-specific password required", the message now explains that an app
  password (not the Google Account password) is needed and where to make one.

## 0.1.2

- iCloud: fixed "Authentication Failed" with a correct app-specific password. Laser Email now signs in with the
  user name Apple's settings call for (the part before the @, then the full address) and cleans invisible
  characters that come along when a password is pasted.
- A refused sign-in now says what to check (an app-specific password for iCloud, an app password for Gmail).
- CI fuzzing runs a fixed number of inputs instead of a time limit.

## 0.1.1

- New app icon: a neon-red envelope on a glass tile.

## 0.1.0

First release.

- Gmail (incl. Google Workspace), iCloud and other IMAP accounts, side by side in one unified inbox (All Inboxes,
  Flagged, Drafts, Sent) plus each account's folders.
- New mail arrives instantly (IMAP IDLE), with notifications and a Dock badge.
- Fast local search across all accounts, including message text.
- Privacy: remote images blocked until you load them, tracking pixels always blocked, tracking tags removed from
  links, no scripts in messages, quiet outgoing mail (no app name, time zone or computer name), no read receipts,
  passwords in the Keychain, mail stored only on your Mac.
- Read, reply, reply all, forward (with attachments), archive, delete, junk, flag, mark read/unread, move, save
  attachments; Mail's keyboard shortcuts and swipe actions.
- A native macOS mail layout: three columns, sidebar with Favorites and accounts, toolbar, compose window.
