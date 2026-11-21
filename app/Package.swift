// swift-tools-version:5.10
import PackageDescription

let package = Package(
    name: "LaserEmail",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "LaserEmail", targets: ["LaserEmail"]),
        .executable(name: "LaserEmailUpdater", targets: ["LaserEmailUpdater"]),
        .library(name: "LaserKit", targets: ["LaserKit"]),
    ],
    targets: [
        // Pure Swift: the daemon client, wire types and the offline update checks. No UI, no network.
        .target(name: "LaserKit"),
        .executableTarget(name: "LaserEmail", dependencies: ["LaserKit"]),
        // The update helper: the only Swift code allowed on the network (GitHub only).
        .executableTarget(name: "LaserEmailUpdater", dependencies: ["LaserKit"]),
        .testTarget(name: "LaserKitTests", dependencies: ["LaserKit"]),
    ]
)
