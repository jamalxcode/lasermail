# Laser Messenger: design

Laser Email becomes **Laser Messenger**: a private, owner-less, peer-to-peer instant messenger for Mac, iPhone
and Android. No company runs it and no server is required. People are identified by keys (like Bitcoin
addresses); spam is stopped by permission and by a currency of computing work that is earned by helping the
network (relaying, storing, seeding, sending notifications).

Status: **design agreed, not built yet.** Laser Email stays released from `main` until Laser Messenger 0.2.0
works; it is built on a branch and Laser Email then updates into it.

## 1. The owner's decisions (2026-11-21)

| # | Topic | Decision |
|---|---|---|
| Q1 | Who can use it | Anyone, like Bitcoin: identity = key pair. |
| Q2 | Users | Everyone who registers (makes a key). |
| Q3 | Identity | Usernames bound to public keys / addresses. |
| Q4 | Groups | Yes. |
| Q5 | Devices | Several devices per person, by keys. |
| Q6–7 | Attachments | Anything, as large as possible; torrent technology for large files. |
| Q8 | Where files live | On the users' own storage, and on other nodes holding pieces (torrent style). |
| Q9–11 | Calls, streaming, screen sharing | Text only first; voice and video later; no streaming or screen sharing. |
| Q12 | Typing, read receipts, presence | Yes. |
| Q13–15 | Servers | None. Peer to peer. Any client can become a super node; no single node (not even the owner's) is essential. |
| Q16 | Protocol | Any open-source protocol that meets these needs. |
| Q17 | Other apps | No interoperability with other messengers. |
| Q18 | Priorities | 1 privacy, 2 simplicity, 3 speed, 4 features, 5 free. |
| Q19 | Platforms | Mac, iPhone, Android. |
| Q20 | History | Kept only on the person's own devices; the owner keeps or erases it. |
| — | Two identities | A **permissioned private address** (only accepted contacts can deliver to it) and an **open public name** (anyone can send; it lands in Requests until promoted to accepted contact). |
| N1 | Name uniqueness | First come, first served on a shared peer-to-peer ledger; on a collision the name gets a short key code (`jamal#k7f3`). |
| N2 | Private address on accept | Each accepted contact gets its **own** private address (a leak exposes one contact only). |
| N3 | Lost devices | Restore from a 12-word recovery phrase. |
| N4 | Leaked key | New identity (per-device keys limit the damage: losing one phone doesn't hand over the identity). |
| N5 | Directory | No directory: public names are shared like a phone number or QR code. |
| N6 | Cost of messaging a stranger | Paid in credits (see the economy), refunded if accepted. |
| N7 | Before acceptance | Strangers can send short text only (1,000 characters). |
| N8 | Full Requests box (500) | Drop the oldest. |
| N9 | Blocking | Block a key; optionally share block lists with contacts. |
| N10 | Offline delivery | Any node volunteering storage holds encrypted messages for a limited time. |
| N11 | Holding time | 7 days. |
| N12 | Phone notifications | Any volunteer super node can send pushes (Apple/Google); the user picks one; it sees no content or sender. |
| N13 | IP addresses | Hidden by default by routing through relay nodes; direct connections allowed for trusted contacts. |
| N14 | Group membership | Any member can add people; whoever added someone, or the creator, can remove them. |
| N15 | Group size | Unlimited: full encryption up to 1,000 posters; larger groups become broadcast channels. |
| N16 | Seeding | Any node can seed encrypted file pieces for credits (it can't read them). |
| N17 | Erase | "Erase for everyone" sends a request to the other people's devices (it can't be forced). |
| N18 | Licence | Open source, MIT/Apache. |
| N19 | iPhone | Pay Apple's $99/year when the iPhone app is ready. |
| N20 | Repo | Turn `jamalxcode/lasermail` into the messenger. |

### The economy: computing-power credits

| # | Decision |
|---|---|
| E1 | Credits are earned by **relaying** messages, **storing** messages for offline people, **seeding** file pieces and **sending push notifications** for others. No puzzle mining. |
| E2 | Work is proven by **signed receipts** (the downloader/recipient signs per piece or message) plus **random spot checks** that a node really holds what it claims. |
| E3 | Balances are kept on a **ledger run by super nodes**, agreeing by vote weighted by credits earned. |
| E4 | Credits **can't be transferred** between people; you spend what your own nodes earned. |
| E5 | A **phone links to its owner's super node** (Mac mini, laptop) and spends that node's balance. |
| E6 | **No free allowance** for new identities. |
| E7 | Credits are spent on: claiming/renewing a **name**; a **first message to a stranger** (refunded if accepted); **storing messages** while you're offline; having **your files seeded** by others. |
| E8 | Messages between **accepted contacts are free**. |
| E9 | Names cost a fixed price and renew yearly; unused names expire. |
| E10 | Seeding: the uploader pays per GB per day kept available; seeders earn per GB served. |
| E11 | Seeders set a disk limit (default 10 GB) and can pause; they hold pieces, never whole files. |
| E12 | Phones seed only when charging and on Wi-Fi. |
| E13 | Earnings scale with uptime and work done; part-time laptops earn too. Cheaters caught by spot checks lose credits. |
| E14 | Super nodes and seeders see nothing readable, and, through relays, not who talks to whom. |
| E15 | Reported file pieces (reported by that chat's members) are dropped network-wide. |

### Open questions

- **Phone-only people (E4 + E5 + E6):** without a computer running a node, a person can't claim a name, message a
  stranger or have messages stored while offline. Accepted contacts still talk for free. Confirm, or allow
  credits to be gifted / a small starter allowance.
- **Ledger security at small scale:** a vote weighted by earned credits is safe only when no one controls most of
  the earning power. Early on, the network is small; a launch period with a founding set of nodes may be
  needed (it conflicts with "owner-less" and needs a decision when the economy is switched on).

## 2. Architecture

```
Every app is a node. Any node can turn on super node mode (and earn credits).

Laser core (Go, one engine for every platform; Mac via a daemon, phones via gomobile)
├─ Network     libp2p: identities as keys, peer discovery (Kademlia DHT), NAT hole punching,
│              circuit relays (default path, hides IPs), QUIC/TCP/WebSocket transports
├─ Delivery    direct to online peers through relays; otherwise to mailbox nodes (super nodes
│              near the recipient's mailbox key in the DHT) that hold encrypted envelopes 7 days
├─ Encryption  X3DH-style key agreement + Double Ratchet per contact; sender keys for groups
│              (MLS later); everything signed by device keys certified by the identity key
├─ Identity    Ed25519 identity key from a 12-word recovery phrase; per-device keys;
│              one pairwise private address per accepted contact; one public name
├─ Names       first-come claims recorded on the ledger; collisions shown with #code
├─ Ledger      credit receipts and balances, kept by super nodes (consensus weighted by
│              earned credits); name claims; reported-piece list
├─ Files       encrypt file → split into pieces → BitTorrent (anacrolix/torrent); key in the
│              message; seeders hold pieces they can't read and earn per GB served
├─ Push        a volunteer super node sends wake-ups through Apple/Google push; it sees only
│              an opaque device token and "wake up"
└─ Storage     messages only on the person's devices (encrypted SQLite); erase on request

Apps: Mac + iPhone in SwiftUI (reusing Laser Email's shell, CI, release and updater);
      Android in Kotlin/Compose; all on the same Go core.
```

**Why libp2p** (rather than Waku, Session, Nostr, Matrix, XMPP): it gives identities-as-keys, DHT discovery,
NAT traversal and relays without any operator, in Go, on phones. Waku adds a store protocol but assumes its own
fleet; Session needs staked service nodes; Nostr and Matrix need servers; XMPP needs servers. Laser's own small
protocols (mailbox, names, credits) sit on top.

## 3. Message flow

1. **Add a contact by public name**: resolve the name on the ledger → public key → send a short text request,
   paying the stranger fee (refunded if accepted). It waits in the recipient's Requests (max 500, oldest
   dropped).
2. **Accept**: the recipient creates a private address just for this contact and sends it with its key bundle;
   from then on messages go to that address, free, end-to-end encrypted.
3. **Send**: if the peer is online, deliver through a relay; otherwise post the encrypted envelope to the
   recipient's mailbox nodes (7 days). The recipient's devices fetch from their mailbox nodes when they come
   online; a push wakes phones.
4. **Files**: encrypt, split, seed; the message carries the torrent info and key; recipients download from all
   seeders (the sender, chat members, paid seeders).

## 4. Build plan

1. **0.2.0-alpha, free test network:** Go core with identity and recovery phrase, per-contact private
   addresses, public names (local, `#code` always shown), Requests and acceptance, 1:1 and group text with
   end-to-end encryption, relays, mailbox nodes; `laser` command-line node with super node mode; Mac app.
2. **0.3:** iPhone app (TestFlight), push through volunteer super nodes.
3. **0.4:** Android app.
4. **0.5:** files over BitTorrent with encryption.
5. **0.6:** the credit economy (receipts, spot checks, ledger, name claims, prices) switched on.
6. **Later:** voice and video calls.
