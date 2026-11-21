import AppKit
import LaserKit

/// Laser Email ▸ Check for Updates…, modelled on laserdot-go: the bundled
/// "Laser Email Updater" helper (the only Swift code with network access)
/// fetches the newest release and its build proof from GitHub; this app then
/// checks the download offline (proof, bundle id, version) and, if the user
/// agrees, swaps itself and relaunches. Nothing is checked in the background.
@MainActor
final class UpdateController {
    static let shared = UpdateController()
    private var busy = false

    static var helper: URL {
        Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/Laser Email Updater.app/Contents/MacOS/LaserEmailUpdater")
    }

    static var currentVersion: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "?"
    }

    func check(_ model: AppModel) async {
        guard !busy else { return }
        busy = true
        defer { busy = false; model.updateProgress = nil }
        guard FileManager.default.isExecutableFile(atPath: Self.helper.path) else {
            alert("Updates work in the installed app", "This copy of Laser Email has no updater (it was not built as an app bundle).")
            return
        }
        model.updateProgress = "Checking for updates…"
        let statusFile = Updater.statusFile
        try? FileManager.default.createDirectory(at: statusFile.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? FileManager.default.removeItem(at: statusFile)
        let ok = await runHelper(statusFile: statusFile)
        guard let status = UpdateStatus.load(from: statusFile) else {
            alert("Could not check for updates", ok ? "The updater gave no answer." : "The updater stopped unexpectedly.", style: .warning)
            return
        }
        switch status.state {
        case .upToDate:
            alert("Laser Email is up to date", "Version \(Self.currentVersion) is the newest version.")
        case .failed:
            alert("Could not check for updates", status.message ?? "Unknown error.", style: .warning)
        case .downloaded:
            guard let file = status.file else { return }
            model.updateProgress = "Checking the update…"
            let updater = Updater()
            let prepared = await Task.detached { Result { try updater.prepare(file: URL(fileURLWithPath: file)) } }.value
            model.updateProgress = nil
            switch prepared {
            case .failure(let e):
                alert("The update can't be installed", e.localizedDescription, style: .warning)
            case .success(let c):
                let a = NSAlert()
                a.messageText = "Laser Email \(c.version) is available"
                a.informativeText = "You have \(Self.currentVersion).\n\n" + (c.whatsNew ?? "") +
                    "\n\nThe update was checked: GitHub's proof shows it was built by Laser Email's own release workflow."
                a.addButton(withTitle: "Install and Relaunch")
                a.addButton(withTitle: "Later")
                guard a.runModal() == .alertFirstButtonReturn else { updater.discard(c); return }
                model.updateProgress = "Installing \(c.version)…"
                let installed = await Task.detached { Result { try updater.install(c) } }.value
                if case .failure(let e) = installed {
                    alert("The update was not installed", e.localizedDescription, style: .warning)
                    return
                }
                updater.relaunch()
                NSApp.terminate(nil)
            }
        }
    }

    private func runHelper(statusFile: URL) async -> Bool {
        await withCheckedContinuation { (cont: CheckedContinuation<Bool, Never>) in
            let p = Process()
            p.executableURL = Self.helper
            p.arguments = ["--app", Bundle.main.bundleURL.path, "--status", statusFile.path]
            p.terminationHandler = { cont.resume(returning: $0.terminationStatus == 0) }
            do { try p.run() } catch { cont.resume(returning: false) }
        }
    }

    private func alert(_ title: String, _ text: String, style: NSAlert.Style = .informational) {
        let a = NSAlert()
        a.alertStyle = style
        a.messageText = title
        a.informativeText = text
        a.runModal()
    }
}
