import Foundation

// Updating Laser Email, modelled on laserdot-go's updater: a separate helper
// app ("Laser Email Updater", the only Swift code allowed on the network)
// downloads the newest release and its build proof from GitHub; this part,
// which runs in the app and never touches the network, checks the download
// and swaps the app.

/// CFBundleShortVersionString, compared numerically part by part
/// ("0.10.0" > "0.9.3").
public struct AppVersion: Comparable, CustomStringConvertible, Sendable {
    public let short: String

    public init(_ short: String) { self.short = short }

    /// The version in a bundle's Info.plist.
    public init?(bundle url: URL) {
        guard let info = NSDictionary(contentsOf: url.appendingPathComponent("Contents/Info.plist")),
              let short = info["CFBundleShortVersionString"] as? String else { return nil }
        self.init(short)
    }

    private var parts: [Int] { short.split(whereSeparator: { !$0.isNumber }).map { Int($0) ?? 0 } }

    public static func < (a: AppVersion, b: AppVersion) -> Bool {
        let (x, y) = (a.parts, b.parts)
        for i in 0..<max(x.count, y.count) {
            let (p, q) = (i < x.count ? x[i] : 0, i < y.count ? y[i] : 0)
            if p != q { return p < q }
        }
        return false
    }

    public static func == (a: AppVersion, b: AppVersion) -> Bool { !(a < b) && !(b < a) }

    public var description: String { short }
}

/// What the helper found, written as JSON for the app to read.
public struct UpdateStatus: Codable, Equatable, Sendable {
    public enum State: String, Codable, Sendable { case upToDate, downloaded, failed }
    public var state: State
    public var latest: String?
    public var file: String?
    public var message: String?

    public init(state: State, latest: String? = nil, file: String? = nil, message: String? = nil) {
        self.state = state
        self.latest = latest
        self.file = file
        self.message = message
    }

    public func save(to url: URL) throws {
        try JSONEncoder().encode(self).write(to: url, options: .atomic)
    }

    public static func load(from url: URL) -> UpdateStatus? {
        (try? Data(contentsOf: url)).flatMap { try? JSONDecoder().decode(UpdateStatus.self, from: $0) }
    }
}

public enum UpdateError: Error, LocalizedError, Equatable {
    case noProof(String)
    case unsupportedFile(String)
    case extractFailed(String)
    case notAnApp
    case wrongApp(String)
    case missingEngine
    case notNewer(current: String, candidate: String)
    case translocated
    case installFailed(String)

    public var errorDescription: String? {
        switch self {
        case .noProof(let m): return "The update was not installed: \(m)."
        case .unsupportedFile(let f): return "\(f) is not a .dmg or .app."
        case .extractFailed(let m): return "Could not open the update: \(m)"
        case .notAnApp: return "The download holds no Laser Email app."
        case .wrongApp(let id): return "The download holds another app (\(id))."
        case .missingEngine: return "The update is incomplete (no laserd inside)."
        case .notNewer(let c, let n): return "The update (\(n)) is not newer than this version (\(c))."
        case .translocated:
            return "macOS is running Laser Email from a temporary copy. Move Laser Email to the Applications folder, open it from there, and check again."
        case .installFailed(let m): return "Could not install the update: \(m)"
        }
    }
}

/// A downloaded update, unpacked and checked, ready to install.
public struct UpdateCandidate: Sendable {
    public let app: URL
    public let version: AppVersion
    let workDir: URL

    /// What's new in this version (Contents/Resources/WhatsNew.txt).
    public var whatsNew: String? {
        (try? String(contentsOf: app.appendingPathComponent("Contents/Resources/WhatsNew.txt"), encoding: .utf8))?
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

public final class Updater: @unchecked Sendable {
    public static let repo = "jamalxcode/lasermail"
    public static let bundleID = "company.sala.laser-email"

    /// The running app.
    public let current: URL
    public let currentVersion: AppVersion?
    /// Where the version before the last update is kept.
    public let previousDir: URL
    /// Who must have built an update (nil: no proof needed — tests only).
    public let proof: ReleaseProof.Identity?

    public static var supportDir: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
            .appendingPathComponent("Laser Email")
    }

    /// Where the helper puts downloads and its status file.
    public static var downloadsDir: URL { supportDir.appendingPathComponent("Updates") }
    public static var statusFile: URL { downloadsDir.appendingPathComponent("update.json") }

    public static var defaultPreviousDir: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
            .appendingPathComponent("Laser Email Previous Version")
    }

    public init(current: URL = Bundle.main.bundleURL, previousDir: URL = Updater.defaultPreviousDir,
                proof: ReleaseProof.Identity? = .laserEmail) {
        self.current = current.standardizedFileURL
        self.currentVersion = AppVersion(bundle: current)
        self.previousDir = previousDir
        self.proof = proof
    }

    // MARK: check

    /// Checks the proof, unpacks `file` (.dmg or .app) privately and checks the
    /// app inside. Nothing is installed yet.
    public func prepare(file: URL) throws -> UpdateCandidate {
        if let proof {
            do {
                try ReleaseProof.verify(file: file, identity: proof)
            } catch let e as ProofError {
                throw UpdateError.noProof(e.message)
            }
        }
        let fm = FileManager.default
        let work = fm.temporaryDirectory.appendingPathComponent("laser-update-\(UUID().uuidString)")
        try fm.createDirectory(at: work, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        do {
            let app = try unpack(file, into: work)
            return try check(app, workDir: work)
        } catch {
            try? fm.removeItem(at: work)
            throw error
        }
    }

    func unpack(_ file: URL, into work: URL) throws -> URL {
        switch file.pathExtension.lowercased() {
        case "app":
            let dest = work.appendingPathComponent(file.lastPathComponent)
            try run("/usr/bin/ditto", [file.path, dest.path])
            return dest
        case "dmg":
            let mnt = work.appendingPathComponent("mnt")
            do {
                try run("/usr/bin/hdiutil", ["attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt.path, file.path])
            } catch {
                throw UpdateError.extractFailed("\(error.localizedDescription)")
            }
            defer { _ = try? run("/usr/bin/hdiutil", ["detach", mnt.path, "-force"]) }
            guard let app = try FileManager.default.contentsOfDirectory(at: mnt, includingPropertiesForKeys: nil)
                .first(where: { $0.pathExtension == "app" }) else { throw UpdateError.notAnApp }
            let dest = work.appendingPathComponent(app.lastPathComponent)
            try run("/usr/bin/ditto", [app.path, dest.path])
            return dest
        default:
            throw UpdateError.unsupportedFile(file.lastPathComponent)
        }
    }

    func check(_ app: URL, workDir: URL) throws -> UpdateCandidate {
        guard let info = NSDictionary(contentsOf: app.appendingPathComponent("Contents/Info.plist")) else { throw UpdateError.notAnApp }
        let id = info["CFBundleIdentifier"] as? String ?? "?"
        guard id == Self.bundleID else { throw UpdateError.wrongApp(id) }
        guard FileManager.default.isExecutableFile(atPath: app.appendingPathComponent("Contents/MacOS/laserd").path) else {
            throw UpdateError.missingEngine
        }
        guard let v = AppVersion(bundle: app) else { throw UpdateError.notAnApp }
        if let cur = currentVersion, !(cur < v) { throw UpdateError.notNewer(current: cur.short, candidate: v.short) }
        return UpdateCandidate(app: app, version: v, workDir: workDir)
    }

    // MARK: install

    /// Replaces the running app with the candidate: the new copy is put
    /// beside it first, the current version is kept in previousDir, then the
    /// new one takes its place. Any failure puts the current one back.
    /// The caller relaunches (`relaunch()`).
    public func install(_ c: UpdateCandidate) throws {
        let fm = FileManager.default
        defer { try? fm.removeItem(at: c.workDir) }
        if current.path.contains("/AppTranslocation/") { throw UpdateError.translocated }
        let staged = current.deletingLastPathComponent().appendingPathComponent(".\(current.lastPathComponent).new")
        try? fm.removeItem(at: staged)
        do {
            try run("/usr/bin/ditto", [c.app.path, staged.path])
        } catch {
            throw UpdateError.installFailed("can't write to \(current.deletingLastPathComponent().path) (\(error.localizedDescription))")
        }
        _ = try? run("/usr/bin/xattr", ["-dr", "com.apple.quarantine", staged.path])
        try fm.createDirectory(at: previousDir, withIntermediateDirectories: true)
        let previous = previousDir.appendingPathComponent(current.lastPathComponent)
        try? fm.removeItem(at: previous)
        do {
            try fm.moveItem(at: current, to: previous)
        } catch {
            try? fm.removeItem(at: staged)
            throw UpdateError.installFailed(error.localizedDescription)
        }
        do {
            try fm.moveItem(at: staged, to: current)
        } catch {
            try? fm.moveItem(at: previous, to: current) // put the old one back
            try? fm.removeItem(at: staged)
            throw UpdateError.installFailed(error.localizedDescription)
        }
    }

    /// Opens the app at `current` again a moment after this process exits.
    public func relaunch() {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/bin/sh")
        p.arguments = ["-c", "sleep 1.5; /usr/bin/open -n \"$0\"", current.path]
        try? p.run()
    }

    public func discard(_ c: UpdateCandidate) { try? FileManager.default.removeItem(at: c.workDir) }

    @discardableResult
    func run(_ tool: String, _ args: [String]) throws -> String {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: tool)
        p.arguments = args
        let out = Pipe()
        p.standardOutput = out
        p.standardError = out
        try p.run()
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        let text = String(decoding: data, as: UTF8.self)
        guard p.terminationStatus == 0 else {
            throw UpdateError.extractFailed("\(URL(fileURLWithPath: tool).lastPathComponent) failed: \(text.trimmingCharacters(in: .whitespacesAndNewlines))")
        }
        return text
    }
}
