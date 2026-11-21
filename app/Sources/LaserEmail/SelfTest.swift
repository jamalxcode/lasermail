import AppKit
import LaserKit
import SwiftUI

/// LASER_SELFTEST=1 (CI, with laserd --demo): walk the real UI so the job can
/// screenshot it. Each "SELFTEST READY <name>" line asks the script for a
/// screenshot; the app waits for "<report>.<name>.done" before moving on.
@MainActor
enum SelfTest {
    static func run(_ model: AppModel) async {
        let out = ProcessInfo.processInfo.environment["LASER_SELFTEST_REPORT"] ?? "/tmp/laser-selftest.txt"
        var report: [String] = []
        func check(_ name: String, _ ok: Bool) { report.append("\(ok ? "PASS" : "FAIL") \(name)") }
        func shot(_ name: String) async {
            try? await Task.sleep(nanoseconds: 1_200_000_000) // let rendering settle
            print("SELFTEST READY \(name)")
            fflush(stdout)
            let done = out + "." + name + ".done"
            for _ in 0..<300 where !FileManager.default.fileExists(atPath: done) {
                try? await Task.sleep(nanoseconds: 100_000_000)
            }
        }
        func waitOpen(_ id: Int64) async {
            for _ in 0..<100 where model.openMessage?.message.id != id || model.renderedMessageID != id {
                try? await Task.sleep(nanoseconds: 100_000_000)
            }
        }

        try? await Task.sleep(nanoseconds: 800_000_000)
        if let w = NSApp.windows.first(where: { $0.isVisible && $0.canBecomeMain }), let screen = w.screen ?? NSScreen.main {
            w.setFrame(screen.visibleFrame.insetBy(dx: 24, dy: 16), display: true, animate: false)
        }
        check("two accounts", model.accounts.count == 2)
        check("unified inbox lists both accounts", Set(model.messages.map(\.accountId)).count == 2)
        check("unread badge", (model.counts["inbox"] ?? 0) == 4)

        if let sale = model.messages.first(where: { $0.subject.hasPrefix("Your weekend") }) {
            model.selectedMessageID = sale.id
            await waitOpen(sale.id)
            check("newsletter opened and drawn", model.openMessage?.message.id == sale.id && model.renderedMessageID == sale.id)
            check("trackers blocked", model.openMessage?.privacy.trackers.count == 2)
            check("no tracking parameters", model.openMessage?.html.contains("utm_") == false)
            await shot("privacy")
        } else {
            check("newsletter present", false)
        }

        if let m = model.messages.first(where: { $0.subject.hasPrefix("Q4 roadmap") }) {
            model.selectedMessageID = m.id
            await waitOpen(m.id)
            check("attachment listed", model.openMessage?.files.first?.name == "Q4 Roadmap.pdf")
            await shot("message")
            model.pendingCompose = ComposeRequest(kind: .reply, messageID: m.id)
            for _ in 0..<50 where !NSApp.windows.contains(where: { $0.isVisible && $0.title.hasPrefix("Re:") }) {
                try? await Task.sleep(nanoseconds: 100_000_000)
            }
            let compose = NSApp.windows.first { $0.isVisible && $0.title.hasPrefix("Re:") }
            check("reply window opened", compose != nil)
            if let compose, let screen = compose.screen ?? NSScreen.main {
                let f = screen.visibleFrame
                compose.setFrame(NSRect(x: f.midX - 120, y: f.minY + 40, width: 680, height: 520), display: true)
                compose.makeKeyAndOrderFront(nil)
            }
            await shot("compose")
        }

        try? report.joined(separator: "\n").appending("\n").write(toFile: out, atomically: true, encoding: .utf8)
        print("SELFTEST DONE")
        fflush(stdout)
    }
}
