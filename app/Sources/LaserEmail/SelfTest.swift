import AppKit
import LaserKit
import SwiftUI

/// LASER_SELFTEST=1 (CI, with laserd --demo): walk the real UI so the job can
/// screenshot it. It selects the demo newsletter (privacy banner), writes a
/// report, and prints READY lines the script waits for.
@MainActor
enum SelfTest {
    static func run(_ model: AppModel) async {
        let out = ProcessInfo.processInfo.environment["LASER_SELFTEST_REPORT"] ?? "/tmp/laser-selftest.txt"
        var report: [String] = []
        func check(_ name: String, _ ok: Bool) { report.append("\(ok ? "PASS" : "FAIL") \(name)") }

        try? await Task.sleep(nanoseconds: 800_000_000)
        NSApp.windows.first { $0.isVisible && $0.canBecomeMain }?.setFrame(NSRect(x: 40, y: 40, width: 1280, height: 800), display: true)
        check("two accounts", model.accounts.count == 2)
        check("unified inbox lists both accounts", Set(model.messages.map(\.accountId)).count == 2)
        check("unread badge", (model.counts["inbox"] ?? 0) == 4)

        if let sale = model.messages.first(where: { $0.subject.hasPrefix("Your weekend") }) {
            model.selectedMessageID = sale.id
            for _ in 0..<40 where model.openMessage?.message.id != sale.id { try? await Task.sleep(nanoseconds: 100_000_000) }
            check("newsletter opened", model.openMessage?.message.id == sale.id)
            check("trackers blocked", model.openMessage?.privacy.trackers.count == 2)
            check("no tracking parameters", model.openMessage?.html.contains("utm_") == false)
        } else {
            check("newsletter present", false)
        }
        try? await Task.sleep(nanoseconds: 1_500_000_000)
        print("SELFTEST READY main")
        fflush(stdout)

        // Second shot: a personal message with an attachment.
        if let m = model.messages.first(where: { $0.subject.hasPrefix("Q4 roadmap") }) {
            model.selectedMessageID = m.id
            try? await Task.sleep(nanoseconds: 1_500_000_000)
            check("attachment listed", model.openMessage?.files.first?.name == "Q4 Roadmap.pdf")
        }
        print("SELFTEST READY attachment")
        fflush(stdout)

        try? report.joined(separator: "\n").appending("\n").write(toFile: out, atomically: true, encoding: .utf8)
        print("SELFTEST DONE")
        fflush(stdout)
    }
}
