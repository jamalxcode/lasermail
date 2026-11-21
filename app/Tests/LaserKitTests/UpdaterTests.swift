import Foundation
@testable import LaserKit
import XCTest

final class UpdaterTests: XCTestCase {
    var dir: URL!

    override func setUpWithError() throws {
        dir = FileManager.default.temporaryDirectory.appendingPathComponent("upd-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    override func tearDown() { try? FileManager.default.removeItem(at: dir) }

    /// A minimal app bundle.
    func makeApp(_ name: String, version: String, id: String = Updater.bundleID, engine: Bool = true, marker: String = "") throws -> URL {
        let app = dir.appendingPathComponent(name).appendingPathComponent("Laser Email.app")
        let macos = app.appendingPathComponent("Contents/MacOS")
        try FileManager.default.createDirectory(at: macos, withIntermediateDirectories: true)
        let info: NSDictionary = ["CFBundleIdentifier": id, "CFBundleShortVersionString": version]
        info.write(to: app.appendingPathComponent("Contents/Info.plist"), atomically: true)
        if engine {
            let d = macos.appendingPathComponent("laserd")
            try Data("#!/bin/sh\n".utf8).write(to: d)
            try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: d.path)
        }
        try Data(marker.utf8).write(to: macos.appendingPathComponent("marker"))
        return app
    }

    func testVersions() {
        XCTAssertLessThan(AppVersion("0.1.3"), AppVersion("0.1.4"))
        XCTAssertLessThan(AppVersion("0.9.9"), AppVersion("0.10.0"))
        XCTAssertLessThan(AppVersion("0.1"), AppVersion("0.1.1"))
        XCTAssertEqual(AppVersion("1.0"), AppVersion("1.0.0"))
        XCTAssertFalse(AppVersion("1.2.0") < AppVersion("1.1.9"))
    }

    func testStatusRoundTrip() throws {
        let f = dir.appendingPathComponent("update.json")
        let s = UpdateStatus(state: .downloaded, latest: "0.1.4", file: "/x/LaserEmail-0.1.4.dmg")
        try s.save(to: f)
        XCTAssertEqual(UpdateStatus.load(from: f), s)
    }

    func testChecksRefuseBadDownloads() throws {
        let current = try makeApp("current", version: "0.1.3")
        let u = Updater(current: current, previousDir: dir.appendingPathComponent("prev"), proof: nil)
        XCTAssertThrowsError(try u.prepare(file: try makeApp("old", version: "0.1.3"))) { e in
            XCTAssertEqual(e as? UpdateError, .notNewer(current: "0.1.3", candidate: "0.1.3"))
        }
        XCTAssertThrowsError(try u.prepare(file: try makeApp("other", version: "9.0", id: "com.example.other"))) { e in
            XCTAssertEqual(e as? UpdateError, .wrongApp("com.example.other"))
        }
        XCTAssertThrowsError(try u.prepare(file: try makeApp("broken", version: "9.0", engine: false))) { e in
            XCTAssertEqual(e as? UpdateError, .missingEngine)
        }
        XCTAssertThrowsError(try u.prepare(file: dir.appendingPathComponent("x.zip"))) { e in
            XCTAssertEqual(e as? UpdateError, .unsupportedFile("x.zip"))
        }
        // With proofs required (the default), nothing without one gets in.
        XCTAssertThrowsError(try Updater(current: current).prepare(file: try makeApp("new", version: "9.0"))) { e in
            guard case UpdateError.noProof = e else { return XCTFail("\(e)") }
        }
    }

    func testInstallSwapsAndKeepsThePreviousVersion() throws {
        let current = try makeApp("Applications", version: "0.1.3", marker: "old")
        let prev = dir.appendingPathComponent("prev")
        let u = Updater(current: current, previousDir: prev, proof: nil)
        let c = try u.prepare(file: try makeApp("download", version: "0.1.4", marker: "new"))
        XCTAssertEqual(c.version, AppVersion("0.1.4"))
        try u.install(c)
        XCTAssertEqual(AppVersion(bundle: current), AppVersion("0.1.4"))
        XCTAssertEqual(try String(contentsOf: current.appendingPathComponent("Contents/MacOS/marker"), encoding: .utf8), "new")
        XCTAssertEqual(AppVersion(bundle: prev.appendingPathComponent("Laser Email.app")), AppVersion("0.1.3"))
        XCTAssertFalse(FileManager.default.fileExists(atPath: c.workDir.path), "work folder left behind")
        XCTAssertTrue(FileManager.default.isExecutableFile(atPath: current.appendingPathComponent("Contents/MacOS/laserd").path))
    }

    /// Release CI: the updater helper just downloaded the published release
    /// (LASER_UPDATE_LIVE_DMG, with its proof beside it); the app's checks
    /// accept it as an update for an older install.
    func testThePublishedReleaseIsAccepted() throws {
        guard let dmg = ProcessInfo.processInfo.environment["LASER_UPDATE_LIVE_DMG"] else {
            throw XCTSkip("set by the release workflow")
        }
        let current = try makeApp("installed", version: "0.0.1")
        let u = Updater(current: current, previousDir: dir.appendingPathComponent("prev"))
        let c = try u.prepare(file: URL(fileURLWithPath: dmg))
        XCTAssertTrue(FileManager.default.fileExists(atPath: c.app.appendingPathComponent("Contents/Helpers/Laser Email Updater.app").path))
        try u.install(c)
        XCTAssertEqual(AppVersion(bundle: current), c.version)
    }
}
