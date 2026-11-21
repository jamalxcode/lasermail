// swift-tools-version:5.10
import PackageDescription

let package = Package(
    name: "LaserEmail",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "LaserEmail", targets: ["LaserEmail"]),
        .library(name: "LaserKit", targets: ["LaserKit"]),
    ],
    targets: [
        // Pure Swift: the daemon client and the wire types. No UI, no network.
        .target(name: "LaserKit"),
        .executableTarget(name: "LaserEmail", dependencies: ["LaserKit"]),
        .testTarget(name: "LaserKitTests", dependencies: ["LaserKit"]),
    ]
)
