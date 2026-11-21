import Foundation
import LaserKit

/// Finds laserd's socket and starts the bundled laserd when nothing answers.
/// The engine keeps running in the background while the app is open and is
/// stopped when the app quits.
final class DaemonLauncher: @unchecked Sendable {
    let dataDir: String
    let socketPath: String
    private var process: Process?

    init() {
        let env = ProcessInfo.processInfo.environment
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        dataDir = env["LASER_DATA"] ?? support.appendingPathComponent("Laser Email").path
        socketPath = env["LASER_SOCKET"] ?? (dataDir as NSString).appendingPathComponent("laserd.sock")
    }

    private var laserdURL: URL? {
        if let p = ProcessInfo.processInfo.environment["LASERD"] { return URL(fileURLWithPath: p) }
        if let u = Bundle.main.url(forAuxiliaryExecutable: "laserd") { return u }
        // Next to the app binary (swift run during development).
        let here = URL(fileURLWithPath: CommandLine.arguments[0]).deletingLastPathComponent().appendingPathComponent("laserd")
        return FileManager.default.isExecutableFile(atPath: here.path) ? here : nil
    }

    func connect(_ client: DaemonClient) async throws {
        if client.isConnected { return }
        if (try? client.connect()) != nil { return }
        guard let bin = laserdURL else { throw DaemonError.notRunning("laserd was not found in the app bundle") }
        let p = Process()
        p.executableURL = bin
        var args = ["--data", dataDir, "--socket", socketPath]
        if ProcessInfo.processInfo.environment["LASER_DEMO"] != nil { args.append("--demo") }
        p.arguments = args
        try FileManager.default.createDirectory(atPath: dataDir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let log = (dataDir as NSString).appendingPathComponent("laserd.log")
        FileManager.default.createFile(atPath: log, contents: nil, attributes: [.posixPermissions: 0o600])
        if let h = FileHandle(forWritingAtPath: log) {
            h.seekToEndOfFile()
            p.standardError = h
            p.standardOutput = h
        }
        try p.run()
        process = p
        for _ in 0..<100 {
            if (try? client.connect()) != nil { return }
            if !p.isRunning { throw DaemonError.notRunning("laserd exited; see \(log)") }
            try await Task.sleep(nanoseconds: 50_000_000)
        }
        throw DaemonError.notRunning("laserd did not start in time")
    }

    func stop() {
        process?.terminate()
        process = nil
    }

    deinit { stop() }
}
