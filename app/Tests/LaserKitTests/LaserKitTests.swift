import XCTest
@testable import LaserKit

final class LaserKitTests: XCTestCase {
    func testDecodesWireShapes() throws {
        let json = """
        {"id":7,"account_id":1,"mailbox_id":3,"uid":9,"message_id":"a@b","thread_key":"a@b","subject":"Hi",
         "from":{"name":"Ann","email":"ann@a.test"},"to":null,"date":"2026-11-01T10:00:00Z","seen":false,
         "flagged":true,"answered":false,"has_attachments":true,"size":10,"snippet":"hello","has_body":true,
         "mailbox":"Inbox","account":"me@gmail.com","html":"<p>x</p>",
         "privacy":{"trackers":["pixel"],"remote_blocked":2,"links_cleaned":1,"removed":0},
         "remote_images":false,"attachments":[{"index":0,"name":"a.pdf","type":"application/pdf","size":6,"inline":false}],
         "read_receipt_requested":true}
        """.data(using: .utf8)!
        let v = try DaemonClient.decoder.decode(MessageView.self, from: json)
        XCTAssertEqual(v.message.id, 7)
        XCTAssertEqual(v.message.from.display, "Ann")
        XCTAssertTrue(v.message.to.isEmpty)
        XCTAssertTrue(v.message.flagged)
        XCTAssertEqual(v.privacy.trackers, ["pixel"])
        XCTAssertEqual(v.files.count, 1)
        XCTAssertTrue(v.readReceiptRequested)
        XCTAssertEqual(privacySummary(v.privacy, remoteLoaded: false), "Laser blocked 1 tracker, hid 2 remote images and cleaned 1 link.")
    }

    func testGoTimes() {
        XCTAssertNotNil(parseGoTime("2026-11-21T06:54:36.123456789Z"))
        XCTAssertNotNil(parseGoTime("2026-11-21T09:54:36+03:00"))
        XCTAssertNotNil(parseGoTime("0001-01-01T00:00:00Z"))
        XCTAssertEqual(parseGoTime("2026-11-21T06:54:36.5Z"), parseGoTime("2026-11-21T06:54:36Z"))
    }

    func testEncodesSnakeCase() throws {
        var q = Query()
        q.accountId = 2
        q.role = "inbox"
        let s = String(data: try DaemonClient.encoder.encode(q), encoding: .utf8)!
        XCTAssertTrue(s.contains("\"account_id\":2"), s)
    }

    func testAddresses() {
        let a = Draft.parseAddresses("Ann Example <ann@a.test>, b@b.test; \"C\" <c@c.test>")
        XCTAssertEqual(a.map(\.email), ["ann@a.test", "b@b.test", "c@c.test"])
        XCTAssertEqual(a[0].name, "Ann Example")
        XCTAssertEqual(Draft.formatAddresses(a), "Ann Example <ann@a.test>, b@b.test, C <c@c.test>")
    }

    func testInitialsAndDates() {
        XCTAssertEqual(initials(Address(name: "Priya Natarajan", email: "p@x")), "PN")
        XCTAssertEqual(initials(Address(email: "news@shop.test")), "NS")
        let now = Date()
        XCTAssertEqual(listDate(now.addingTimeInterval(-86400), now: now), "Yesterday")
    }

    /// Against a real laserd when LASERD points at the binary.
    func testDemoDaemon() async throws {
        guard let bin = ProcessInfo.processInfo.environment["LASERD"] else { throw XCTSkip("set LASERD") }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("lsr-\(UUID().uuidString.prefix(6))")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let sock = "/tmp/lsr-\(UUID().uuidString.prefix(6)).sock"
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = ["--demo", "--data", dir.path, "--socket", sock]
        try p.run()
        defer { p.terminate() }
        let c = DaemonClient(socketPath: sock)
        for _ in 0..<50 {
            if (try? c.connect()) != nil { break }
            try await Task.sleep(nanoseconds: 100_000_000)
        }
        let accts = try await c.accounts()
        XCTAssertEqual(accts.accounts.count, 2)
        var q = Query()
        q.role = "inbox"
        let inbox = try await c.list(q)
        XCTAssertEqual(inbox.count, 9)
        let sale = try XCTUnwrap(inbox.first { $0.subject.hasPrefix("Your weekend") })
        let v = try await c.open(sale.id, markSeen: false)
        XCTAssertEqual(v.privacy.trackers.count, 2)
        let boxes = try await c.mailboxes()
        XCTAssertTrue(boxes.contains { $0.role == "inbox" })
    }
}
