# Laser Room: specification

**Laser Room** is a private work table for invited groups: a board of signed, timestamped posts and replies
with attachments. Members are identified by a number their own device produces from its built-in biometric
unlock (Face ID, Touch ID, an Android fingerprint or face, Windows Hello). Nobody's biometrics are collected,
and the number can't be turned back into the person. When the work is done, the owner clears the table.

Status: **specification agreed, not built yet** (2026-11-22). It replaces the Laser Email and Laser Messenger
plans (history in `DECISIONS.md`).

## 1. Identity: a number from the device's biometric unlock

| # | Rule |
|---|---|
| I0 | **Custody rule (owner, 2026-11-22):** biometrics are only the door. Apple, Google or anyone else must never hold, store or sync any Laser Room password or code: the Laser Room client app creates and keeps them. Hence Laser Room is an **installed app** (a web app could only use biometrics through passkeys, which Apple/Google keychains store and sync). |
| I1 | The app asks the device's own biometric check (Face ID, Touch ID, Android fingerprint/face, Windows Hello; the device PIN/password where there is no biometric hardware). Any unlock the device offers counts (owner: 1 a+b+c). |
| I1a | The app keeps all passwords in **its own encrypted vault** on the device, locked by a key in the device's security chip (Secure Enclave, Android Keystore/StrongBox, TPM) that opens only after a biometric check and is marked this-device-only, never synced. No passkeys, no iCloud Keychain, no Google Password Manager. |
| I2 | **Two passwords, two jobs.** Both are created by the app (256-bit random) after a biometric check and kept in its vault (I1a): (1) the **board password**, made by the owner's device when the board is created: it opens and encrypts the board and **is shared** with the people invited (W2); (2) each member's **personal password**, made by their own device for that board: it is **never shared**, it signs everything the member posts, and its one-way hash is the member's **ID number** in that board. Holding the board password lets someone open the board; only the personal password lets them post as themselves. Both are 256-bit and filled in by the app, never typed. Same device + same board → the same personal password and ID number; another board → unrelated ones (owner: 6a, O1, 2026-11-22). |
| I3 | No biometric data is read, stored or sent by Laser Room; the number can't be worked back to the person or their biometric. |
| I4 | The number has to be unique **within the workspace only**; invitations bind one person to one member slot. |
| I5 | The scan is done **once per device** (owner: 2c). After that the device holds the identity: "it's your device; keep it with you." |
| I6 | **New device:** the workspace owner re-admits the person under the same alias (owner: 3b). |
| I7 | **Lost device:** the owner re-admits the person; their old posts stay under their alias, and the re-admission is shown (owner: 4a). |
| I8 | **Aliases:** chosen per workspace, unique within it (owner: 7a). An alias can be a real name or a made-up one; people reveal who they are, if they want to, outside Laser Room (message, email, call) (owner: 8). |
| I9 | Nothing syncs through Apple or Google. A new device gets its own personal password; the owner re-admits the person under the same alias (I6). |

## 2. Workspaces (the work table)

| # | Rule |
|---|---|
| W1 | Workspaces are **private**. The creator (owner) invites people; there is no public listing or sign-up. A collaboration tool, not a public forum. |
| W2 | Joining is **by invitation**: the owner shares the board password (I2) with the person, through any channel they like (message, email, in person, QR code). |
| W3 | **No removal** of members (owner: 11b). The owner controls membership by choosing carefully whom to invite; each member is responsible for their own content. The owner can clear the whole table (W8) and start a new one without someone. |
| W4 | **Ownership can be handed over** to another member. |
| W5 | **Everything is encrypted** with a key derived from the board password; any relay, introduction service or cloud copy sees locked data only (owner: 15a). Anyone who obtains the board password can read the board, so it should be shared privately. |
| W6 | **Sync:** whoever opens the board is brought up to date with the latest version and saves it on their device. Members' copies sync with each other whenever they are online together (owner: 9). |
| W7 | **Self-hosted (owner: G12 c):** each board owner hosts their own board. The desktop app has a **host mode** (e.g. on a Mac mini) that acts as the board's introduction service and keeps the encrypted copy, so the board is available when no member is online; it can't read the board. Without a running host, members sync only when online together. |
| W8 | **Clearing the table:** the owner can delete the whole board when the work is done ("files shared, decisions made, table cleared"). The cloud copy is deleted and every member's copy is wiped the next time it syncs. A modified app could ignore the wipe; the app says so. |

## 3. Posts, replies, attachments

| # | Rule |
|---|---|
| P1 | The board holds **posts and replies** (one level: a post and its replies). |
| P2 | Every post, reply, edit and deletion is **timestamped and signed** by the author's alias/ID number (owner: 6b). Signatures are checked by every member's app. |
| P3 | Others see a post **only once it is submitted**; there's no live typing. |
| P4 | **Editing:** only the author can edit; each edit shows the alias and time. |
| P5 | **Deleting:** only the author can delete their own post. The content is removed; a marker remains with the alias and time of the deletion (owner: 7, 10). |
| P6 | **Attachments:** anything (images, files, text), with no size limit (owner: 11, 14b). Large files sync like the rest; members' devices and the cloud copy must have room. |

## 3a. Gap decisions (owner, 2026-11-22)

| # | Decision |
|---|---|
| G1 | Joining needs the board password **and** the owner's approval. |
| G2 | The owner can **mute** a member (they can read, not post); no removal (W3). |
| G3 | If the owner disappears, the board **continues with no owner**: existing members read and post; owner-only actions (approvals, alias confirmations, re-admissions, clearing) stop. |
| G4 | The device PIN is allowed only on devices without a biometric sensor; those members are shown as "PIN-verified". |
| G5 | The host also stamps each post with its own clock; both times are shown if they differ. |
| G6 | The owner's device confirms each new alias before it is used (unique within the board). |
| G7 | One identity per device; shared devices aren't supported. |
| G8 | Any member can export a signed copy of posts that anyone can verify. |
| G9 | Phones download attachments when opened; desktops can keep everything. |
| G10 | The host keeps a board until it is cleared, or until 90 days without anyone opening it (members are warned first). |
| G11 | Before the table is cleared, members get a warning and a few days to save what they need. |
| G12 | Each owner hosts their own board (W7). |
| G13 | Phone notifications through Apple/Google push, carrying only an empty "wake up" (never content, passwords or codes). |
| G14 | A workspace can hold several boards (topics). |
| G15 | Local search of posts and attachment names. |
| G16 | @alias mentions and simple reactions. |
| G17 | Read receipts are shown. |
| G18 | English and Arabic (right-to-left) from the start. |
| G19 | Lost or stolen device: nothing extra (the device can't be unlocked without the owner's biometric; the person is re-admitted on a new device, I7). |
| G20 | Screenshots and copying can't be prevented; the app says so. |
| D1 | iPhone app distributed through the App Store / TestFlight (Apple distributes; it never holds passwords or codes). |
| D2 | Built with **Flutter**: one app for Mac, Windows, iPhone and Android, using each device's biometric check and security chip. |
| D3 | Hosting: the owner's own computer (host mode) or any independent host they choose (G12). |

## 4. Architecture

```
Laser Room installed app (Flutter: Mac, Windows, iPhone, Android; desktop can run host mode)
├─ Identity     device biometric check → app's own vault (security-chip key, this device only)
│               → personal password + signing key per board; board passwords
├─ Board data   an append-only log of signed events (post, reply, edit, delete, invite, handover,
│               clear) merged the same way on every device (CRDT), so copies always converge
├─ Encryption   a workspace key (shared with each invited member's key); every event and
│               attachment encrypted before it leaves the device
├─ Storage      the board and attachments on each member's device (browser storage, with an
│               export so nothing depends on the browser keeping it)
├─ Sync         direct between members' browsers (WebRTC); an introduction service helps them
│               find each other and relays when networks block direct links (sees only
│               encrypted data; we provide one, anyone can run their own)
└─ Host mode    the owner's computer introduces members, relays, keeps the encrypted copy and
                stamps post times (W7, G5, G10); it can't read the board
```

## 5. Settled questions

- **O1 → superseded:** no passkeys and no syncing through Apple or Google; the app holds its own secrets (I0, I1a).
- **O2 → superseded by G12:** each owner hosts their own board (W7).
- **O3:** Safari wipes browser storage after 7 days without a visit unless the app is installed to the home
  screen; the cloud copy (W7) and installing the app keep boards safe.
