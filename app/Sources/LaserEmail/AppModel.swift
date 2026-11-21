import AppKit
import Foundation
import LaserKit
import Observation
import UserNotifications

/// What the sidebar can select.
enum SidebarItem: Hashable {
    case allInboxes
    case flagged
    case role(String)   // unified Drafts / Sent / …
    case mailbox(Int64) // one account's folder
}

/// The app's state and every action; views stay thin.
@Observable @MainActor
final class AppModel {
    let client: DaemonClient
    private let launcher: DaemonLauncher

    var connected = false
    var connectionError: String?
    var oauthAvailable = false
    var presets: [String: AccountSettings] = [:]

    var accounts: [Account] = []
    var status: [Int64: AccountStatus] = [:]
    var mailboxes: [Mailbox] = []
    var counts: [String: Int] = [:]

    var selection: SidebarItem? = .allInboxes {
        didSet { if oldValue != selection { selectedMessageID = nil; Task { await refreshList() } } }
    }
    var messages: [Message] = []
    var selectedMessageID: Int64? {
        didSet { if oldValue != selectedMessageID { Task { await openSelected() } } }
    }
    var openMessage: MessageView?
    var loadingMessage = false
    var searchText = "" {
        didSet { searchTask?.cancel(); searchTask = Task { try? await Task.sleep(nanoseconds: 250_000_000); await refreshList() } }
    }
    var unreadOnly = false { didSet { Task { await refreshList() } } }
    var showAddAccount = false
    /// A compose window MainView should open (used by the self-test).
    var pendingCompose: ComposeRequest?
    var alert: String?

    private var searchTask: Task<Void, Never>?
    private var refreshTask: Task<Void, Never>?

    init() {
        launcher = DaemonLauncher()
        client = DaemonClient(socketPath: launcher.socketPath)
    }

    // MARK: Startup

    func start() async {
        do {
            try await launcher.connect(client)
            let h = try await client.hello()
            oauthAvailable = h.oauth
            presets = h.presets ?? [:]
            connected = true
            connectionError = nil
            try await client.subscribe()
            await refreshAll()
            if accounts.isEmpty { showAddAccount = true }
            Task { await listen() }
            requestNotificationPermission()
            if ProcessInfo.processInfo.environment["LASER_SELFTEST"] != nil { await SelfTest.run(self) }
        } catch {
            connected = false
            connectionError = error.localizedDescription
        }
    }

    private func listen() async {
        for await ev in client.events {
            switch ev.type {
            case "disconnected":
                connected = false
                connectionError = "Reconnecting to the engine…"
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                await start()
                return
            case "new_mail":
                if let a = ev.accountId { await notifyNewMail(account: a, count: ev.count ?? 1) }
                scheduleRefresh()
            case "accounts":
                await refreshAll()
            case "status":
                await refreshAccounts()
            default:
                scheduleRefresh()
            }
        }
    }

    /// Coalesces bursts of sync events into one refresh.
    private func scheduleRefresh() {
        refreshTask?.cancel()
        refreshTask = Task {
            try? await Task.sleep(nanoseconds: 300_000_000)
            if Task.isCancelled { return }
            await refreshSidebar()
            await refreshList()
        }
    }

    // MARK: Loading

    func refreshAll() async {
        await refreshAccounts()
        await refreshSidebar()
        await refreshList()
    }

    func refreshAccounts() async {
        guard let r = try? await client.accounts() else { return }
        accounts = r.accounts
        var s: [Int64: AccountStatus] = [:]
        for (k, v) in r.status { if let id = Int64(k) { s[id] = v } }
        status = s
    }

    func refreshSidebar() async {
        if let m = try? await client.mailboxes() { mailboxes = m }
        if let c = try? await client.counts() { counts = c }
        NSApp.dockTile.badgeLabel = (counts["inbox"] ?? 0) > 0 ? "\(counts["inbox"]!)" : nil
    }

    var query: Query {
        var q = Query()
        switch selection ?? .allInboxes {
        case .allInboxes: q.role = "inbox"
        case .flagged: q.flagged = true
        case .role(let r): q.role = r
        case .mailbox(let id): q.mailboxId = id
        }
        q.unread = unreadOnly
        q.text = searchText.trimmingCharacters(in: .whitespaces)
        if !q.text.isEmpty { q.role = ""; q.mailboxId = 0; q.flagged = false } // search everywhere, like Mail's "All Mailboxes"
        return q
    }

    func refreshList() async {
        do {
            let l = try await client.list(query)
            messages = l
            if let sel = selectedMessageID, !l.contains(where: { $0.id == sel }) { selectedMessageID = nil }
        } catch {
            if connected { alert = error.localizedDescription }
        }
    }

    func openSelected() async {
        guard let id = selectedMessageID else { openMessage = nil; return }
        loadingMessage = true
        defer { loadingMessage = false }
        do {
            let v = try await client.open(id, markSeen: true)
            guard selectedMessageID == id else { return }
            openMessage = v
            if let i = messages.firstIndex(where: { $0.id == id }), !messages[i].seen {
                messages[i].seen = v.message.seen
                await refreshSidebar()
            }
        } catch {
            alert = error.localizedDescription
        }
    }

    func loadImages(always: Bool) async {
        guard let v = openMessage else { return }
        if always { try? await client.setLoadImages(v.message.from.email, on: true) }
        if let nv = try? await client.open(v.message.id, loadImages: true, markSeen: false) { openMessage = nv }
    }

    // MARK: Titles

    var listTitle: String {
        if !searchText.isEmpty { return "Search" }
        switch selection ?? .allInboxes {
        case .allInboxes: return "Inbox"
        case .flagged: return "Flagged"
        case .role(let r): return r.capitalized
        case .mailbox(let id):
            guard let mb = mailboxes.first(where: { $0.id == id }) else { return "Mailbox" }
            if let a = accounts.first(where: { $0.id == mb.accountId }), accounts.count > 1 { return "\(mb.name) — \(a.title)" }
            return mb.name
        }
    }

    var listSubtitle: String {
        let n = messages.count
        let u = messages.filter { !$0.seen }.count
        let ms = n == 1 ? "1 message" : "\(n) messages"
        return u > 0 ? "\(ms), \(u) unread" : ms
    }

    func account(_ id: Int64) -> Account? { accounts.first { $0.id == id } }
    func mailboxes(of account: Int64) -> [Mailbox] { mailboxes.filter { $0.accountId == account && $0.role != "all" } }

    var selectedMessage: Message? { messages.first { $0.id == selectedMessageID } }

    // MARK: Actions (on the selected message unless given one)

    private func target(_ id: Int64?) -> Int64? { id ?? selectedMessageID }

    func perform(_ what: String, _ id: Int64? = nil) async {
        guard let id = target(id) else { return }
        let next = neighbour(of: id)
        do {
            switch what {
            case "archive": try await client.archive(id)
            case "delete": try await client.delete(id)
            case "junk": try await client.junk(id)
            default: return
            }
            messages.removeAll { $0.id == id }
            if selectedMessageID == id { selectedMessageID = next }
            await refreshSidebar()
        } catch {
            alert = error.localizedDescription
        }
    }

    /// The message to select after one leaves the list (the next one down, like Mail).
    private func neighbour(of id: Int64) -> Int64? {
        guard let i = messages.firstIndex(where: { $0.id == id }) else { return nil }
        if i + 1 < messages.count { return messages[i + 1].id }
        return i > 0 ? messages[i - 1].id : nil
    }

    func toggleFlag(_ id: Int64? = nil) async {
        guard let id = target(id), let i = messages.firstIndex(where: { $0.id == id }) else { return }
        let on = !messages[i].flagged
        do {
            try await client.setFlag(id, flag: "flagged", on: on)
            messages[i].flagged = on
            if openMessage?.message.id == id { openMessage?.message.flagged = on }
            await refreshSidebar()
        } catch { alert = error.localizedDescription }
    }

    func toggleRead(_ id: Int64? = nil) async {
        guard let id = target(id), let i = messages.firstIndex(where: { $0.id == id }) else { return }
        let on = !messages[i].seen
        do {
            try await client.setFlag(id, flag: "seen", on: on)
            messages[i].seen = on
            await refreshSidebar()
        } catch { alert = error.localizedDescription }
    }

    func move(_ id: Int64, to mailbox: Int64) async {
        do {
            try await client.move(id, to: mailbox)
            messages.removeAll { $0.id == id }
            await refreshSidebar()
        } catch { alert = error.localizedDescription }
    }

    func getMail() async { try? await client.sync() }

    func saveAttachment(_ a: Attachment) async {
        guard let v = openMessage else { return }
        do {
            let path = try await client.saveAttachment(v.message.id, index: a.index)
            NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
        } catch { alert = error.localizedDescription }
    }

    func removeAccount(_ a: Account) async {
        do {
            try await client.removeAccount(a.id)
            if case .mailbox(let id) = selection, mailboxes.first(where: { $0.id == id })?.accountId == a.id { selection = .allInboxes }
            await refreshAll()
        } catch { alert = error.localizedDescription }
    }

    // MARK: Notifications (local only)

    private var canNotify: Bool { Bundle.main.bundleIdentifier != nil }

    private func requestNotificationPermission() {
        guard canNotify else { return }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
    }

    private func notifyNewMail(account: Int64, count: Int) async {
        guard canNotify else { return }
        var q = Query()
        q.accountId = account
        q.role = "inbox"
        q.unread = true
        q.limit = 1
        guard let m = try? await client.list(q).first else { return }
        let c = UNMutableNotificationContent()
        c.title = m.from.display
        c.subtitle = m.displaySubject
        c.body = m.snippet
        c.sound = .default
        try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "msg-\(m.id)", content: c, trigger: nil))
    }
}
