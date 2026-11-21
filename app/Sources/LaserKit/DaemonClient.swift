import Foundation

public enum DaemonError: Error, LocalizedError, Equatable {
    case notRunning(String)
    case server(String)
    case connectionLost

    public var errorDescription: String? {
        switch self {
        case .notRunning(let why): return "Laser Email's engine isn't running (\(why))."
        case .server(let msg): return msg
        case .connectionLost: return "Lost the connection to Laser Email's engine."
        }
    }
}

/// Talks to laserd over its Unix socket: one JSON request per line, answers
/// matched by id, and event lines delivered on `events`. This is the app's
/// only channel to anything outside itself — it has no network code.
public final class DaemonClient: @unchecked Sendable {
    public let socketPath: String
    public let events: AsyncStream<DaemonEvent>
    private let eventsContinuation: AsyncStream<DaemonEvent>.Continuation

    private let lock = NSLock()
    private var fd: Int32 = -1
    private var nextID: Int64 = 0
    private var pending: [Int64: CheckedContinuation<Data, Error>] = [:]

    public static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            if let date = parseGoTime(s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: "bad date \(s)"))
        }
        return d
    }()

    public static let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.keyEncodingStrategy = .convertToSnakeCase
        e.dateEncodingStrategy = .iso8601
        return e
    }()

    public init(socketPath: String) {
        self.socketPath = socketPath
        (events, eventsContinuation) = AsyncStream.makeStream(of: DaemonEvent.self, bufferingPolicy: .bufferingNewest(100))
    }

    public var isConnected: Bool {
        lock.lock(); defer { lock.unlock() }
        return fd >= 0
    }

    /// Connects to the socket (throws `.notRunning` when nothing listens).
    public func connect() throws {
        let s = socket(AF_UNIX, SOCK_STREAM, 0)
        guard s >= 0 else { throw DaemonError.notRunning("socket: \(String(cString: strerror(errno)))") }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let path = Array(socketPath.utf8)
        guard path.count < MemoryLayout.size(ofValue: addr.sun_path) else {
            close(s)
            throw DaemonError.notRunning("socket path too long")
        }
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: path)
            raw[path.count] = 0
        }
        let len = socklen_t(MemoryLayout<sockaddr_un>.size)
        let r = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(s, $0, len) }
        }
        guard r == 0 else {
            let why = String(cString: strerror(errno))
            close(s)
            throw DaemonError.notRunning(why)
        }
        var on: Int32 = 1
        setsockopt(s, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
        lock.lock()
        fd = s
        lock.unlock()
        let reader = Thread { [weak self] in self?.readLoop(s) }
        reader.name = "laserd reader"
        reader.start()
    }

    public func disconnect() {
        lock.lock()
        let s = fd
        fd = -1
        lock.unlock()
        if s >= 0 { shutdown(s, SHUT_RDWR); close(s) }
    }

    private func readLoop(_ s: Int32) {
        var buf = [UInt8](repeating: 0, count: 1 << 16)
        var acc = Data()
        while true {
            let n = read(s, &buf, buf.count)
            if n <= 0 { break }
            acc.append(buf, count: n)
            while let nl = acc.firstIndex(of: 10) {
                let line = Data(acc[acc.startIndex..<nl])
                acc.removeSubrange(acc.startIndex...nl)
                handle(line)
            }
        }
        lock.lock()
        if fd == s { fd = -1; close(s) }
        let waiting = pending
        pending = [:]
        lock.unlock()
        for (_, c) in waiting { c.resume(throwing: DaemonError.connectionLost) }
        eventsContinuation.yield(DaemonEvent(type: "disconnected"))
    }

    private func handle(_ line: Data) {
        guard let obj = try? JSONSerialization.jsonObject(with: line) as? [String: Any] else { return }
        if let ev = obj["event"] {
            if let data = try? JSONSerialization.data(withJSONObject: ev),
               let e = try? Self.decoder.decode(DaemonEvent.self, from: data) {
                eventsContinuation.yield(e)
            }
            return
        }
        guard let id = (obj["id"] as? NSNumber)?.int64Value else { return }
        lock.lock()
        let cont = pending.removeValue(forKey: id)
        lock.unlock()
        guard let cont else { return }
        if let err = obj["error"] as? String, !err.isEmpty {
            cont.resume(throwing: DaemonError.server(err))
            return
        }
        let result = obj["result"] ?? NSNull()
        do {
            cont.resume(returning: try JSONSerialization.data(withJSONObject: result, options: [.fragmentsAllowed]))
        } catch {
            cont.resume(throwing: error)
        }
    }

    private struct Request<A: Encodable>: Encodable {
        let id: Int64
        let op: String
        let args: A
    }

    public struct NoArgs: Encodable, Sendable { public init() {} }

    /// Sends a request and decodes its result.
    public func call<A: Encodable, R: Decodable>(_ op: String, _ args: A, as: R.Type = R.self) async throws -> R {
        lock.lock()
        nextID += 1
        let id = nextID
        let s = fd
        lock.unlock()
        guard s >= 0 else { throw DaemonError.connectionLost }
        var line = try Self.encoder.encode(Request(id: id, op: op, args: args))
        line.append(10)
        let data: Data = try await withCheckedThrowingContinuation { cont in
            lock.lock()
            pending[id] = cont
            lock.unlock()
            let ok = line.withUnsafeBytes { raw -> Bool in
                var off = 0
                while off < raw.count {
                    let n = write(s, raw.baseAddress! + off, raw.count - off)
                    if n <= 0 { return false }
                    off += n
                }
                return true
            }
            if !ok {
                lock.lock()
                let c = pending.removeValue(forKey: id)
                lock.unlock()
                c?.resume(throwing: DaemonError.connectionLost)
            }
        }
        return try Self.decoder.decode(R.self, from: data)
    }

    public func call<R: Decodable>(_ op: String, as: R.Type = R.self) async throws -> R {
        try await call(op, NoArgs(), as: R.self)
    }
}

extension DaemonEvent {
    init(type: String) {
        self.type = type
    }
}

/// Parses Go's RFC 3339 times, with or without nanoseconds.
public func parseGoTime(_ s: String) -> Date? {
    var t = s
    if let dot = t.firstIndex(of: "."), let end = t[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" }) {
        t.removeSubrange(dot..<end)
    }
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime]
    return f.date(from: t)
}

// MARK: - Typed operations

public extension DaemonClient {
    private struct ID: Encodable { let id: Int64 }

    func hello() async throws -> Hello { try await call("hello") }
    func accounts() async throws -> AccountsReply { try await call("accounts") }
    func counts() async throws -> [String: Int] { try await call("counts") }
    func subscribe() async throws { _ = try await call("subscribe", as: Bool.self) }

    func mailboxes(account: Int64 = 0) async throws -> [Mailbox] {
        struct A: Encodable { let accountId: Int64 }
        return try await call("mailboxes", A(accountId: account))
    }

    func list(_ q: Query) async throws -> [Message] { try await call("list", q) }

    func open(_ id: Int64, loadImages: Bool = false, markSeen: Bool = true) async throws -> MessageView {
        struct A: Encodable { let id: Int64; let loadImages: Bool; let markSeen: Bool }
        return try await call("open", A(id: id, loadImages: loadImages, markSeen: markSeen))
    }

    func setFlag(_ id: Int64, flag: String, on: Bool) async throws {
        struct A: Encodable { let id: Int64; let flag: String; let on: Bool }
        _ = try await call("set_flag", A(id: id, flag: flag, on: on), as: Bool.self)
    }

    func archive(_ id: Int64) async throws { _ = try await call("archive", ID(id: id), as: Bool.self) }
    func delete(_ id: Int64) async throws { _ = try await call("delete", ID(id: id), as: Bool.self) }
    func junk(_ id: Int64) async throws { _ = try await call("junk", ID(id: id), as: Bool.self) }

    func move(_ id: Int64, to mailbox: Int64) async throws {
        struct A: Encodable { let id: Int64; let mailboxId: Int64 }
        _ = try await call("move", A(id: id, mailboxId: mailbox), as: Bool.self)
    }

    /// kind: "reply", "reply_all" or "forward".
    func draft(_ kind: String, for id: Int64) async throws -> DraftReply { try await call(kind, ID(id: id)) }

    func send(account: Int64, draft: Draft, attachPaths: [String]) async throws {
        struct A: Encodable { let accountId: Int64; let draft: Draft; let attachPaths: [String] }
        _ = try await call("send", A(accountId: account, draft: draft, attachPaths: attachPaths), as: Bool.self)
    }

    func sync(account: Int64 = 0) async throws {
        struct A: Encodable { let accountId: Int64 }
        _ = try await call("sync", A(accountId: account), as: Bool.self)
    }

    func saveAttachment(_ id: Int64, index: Int, dir: String? = nil) async throws -> String {
        struct A: Encodable { let id: Int64; let index: Int; let dir: String }
        let r: [String: String] = try await call("save_attachment", A(id: id, index: index, dir: dir ?? ""))
        return r["path"] ?? ""
    }

    func inlinePart(_ id: Int64, contentId: String) async throws -> InlinePart {
        struct A: Encodable { let id: Int64; let contentId: String }
        return try await call("inline_part", A(id: id, contentId: contentId))
    }

    func setLoadImages(_ email: String, on: Bool) async throws {
        struct A: Encodable { let email: String; let on: Bool }
        _ = try await call("set_load_images", A(email: email, on: on), as: Bool.self)
    }

    func addAccount(_ n: NewAccount) async throws -> Account { try await call("add_account", n) }
    func removeAccount(_ id: Int64) async throws { _ = try await call("remove_account", ID(id: id), as: Bool.self) }

    func updatePassword(_ id: Int64, password: String) async throws {
        struct A: Encodable { let id: Int64; let password: String }
        _ = try await call("update_password", A(id: id, password: password), as: Bool.self)
    }
}
