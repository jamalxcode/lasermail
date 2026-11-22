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
| I1 | On first use the device creates a passkey (WebAuthn) in its secure chip, unlocked only by the device's own biometric, or its PIN/password when it has no biometric hardware. Any unlock the device offers counts (owner: 1 a+b+c). |
| I2 | **The ID number and access password** are derived on the device: after the biometric scan, the passkey's PRF output for the workspace's salt (a one-way function inside the secure chip) becomes a **unique, complex password** (256-bit, never shown or typed; the web app fills it in itself) that gives access to the workspace, and a hash of it is the member's **ID number**. Same device + same workspace → same password and number; another workspace → unrelated ones (owner: 6a, O1). |
| I3 | No biometric data is read, stored or sent by Laser Room; the number can't be worked back to the person or their biometric. |
| I4 | The number has to be unique **within the workspace only**; invitations bind one person to one member slot. |
| I5 | The scan is done **once per device** (owner: 2c). After that the device holds the identity: "it's your device; keep it with you." |
| I6 | **New device:** the workspace owner re-admits the person under the same alias (owner: 3b). |
| I7 | **Lost device:** the owner re-admits the person; their old posts stay under their alias, and the re-admission is shown (owner: 4a). |
| I8 | **Aliases:** chosen per workspace, unique within it (owner: 7a). An alias can be a real name or a made-up one; people reveal who they are, if they want to, outside Laser Room (message, email, call) (owner: 8). |
| I9 | Passkey syncing through iCloud Keychain / Google Password Manager is allowed when the user has it on; Laser Room works with or without it (owner: O1 a). |

## 2. Workspaces (the work table)

| # | Rule |
|---|---|
| W1 | Workspaces are **private**. The creator (owner) invites people; there is no public listing or sign-up. A collaboration tool, not a public forum. |
| W2 | Joining is **by invitation** from the owner. |
| W3 | **No removal** of members (owner: 11b). The owner controls membership by choosing carefully whom to invite; each member is responsible for their own content. The owner can clear the whole table (W8) and start a new one without someone. |
| W4 | **Ownership can be handed over** to another member. |
| W5 | **Everything is encrypted** with a key only members hold; any relay, introduction service or cloud copy sees locked data only (owner: 15a). |
| W6 | **Sync:** whoever opens the board is brought up to date with the latest version and saves it on their device. Members' copies sync with each other whenever they are online together (owner: 9). |
| W7 | **Cloud copy:** an encrypted copy is kept by the introduction service we run, so the board is available when no member is online (owner: 9, O2 a). It can't read the board. |
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

## 4. Architecture

```
Laser Room web app (one app for Mac, Windows, iPhone, Android; installable to the home screen)
├─ Identity     WebAuthn passkey + PRF → per-workspace ID number; signing key derived from it
├─ Board data   an append-only log of signed events (post, reply, edit, delete, invite, handover,
│               clear) merged the same way on every device (CRDT), so copies always converge
├─ Encryption   a workspace key (shared with each invited member's key); every event and
│               attachment encrypted before it leaves the device
├─ Storage      the board and attachments on each member's device (browser storage, with an
│               export so nothing depends on the browser keeping it)
├─ Sync         direct between members' browsers (WebRTC); an introduction service helps them
│               find each other and relays when networks block direct links (sees only
│               encrypted data; we provide one, anyone can run their own)
└─ Cloud copy   optional encrypted copy in cloud storage (O2), so whoever opens the board next
                gets the latest even when no one else is online
```

## 5. Settled questions

- **O1 → a:** passkey syncing is the user's choice; the biometric unlock produces the workspace password (I2).
- **O2 → a:** the introduction service also keeps the encrypted cloud copy (W7).
- **O3:** Safari wipes browser storage after 7 days without a visit unless the app is installed to the home
  screen; the cloud copy (W7) and installing the app keep boards safe.
