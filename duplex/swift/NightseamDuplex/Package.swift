// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "NightseamDuplex",
    platforms: [.macOS(.v13), .iOS(.v16), .tvOS(.v16), .watchOS(.v9)],
    products: [.library(name: "NightseamDuplex", targets: ["NightseamDuplex"])],
    targets: [
        .target(name: "NightseamDuplex"),
        .testTarget(name: "NightseamDuplexTests", dependencies: ["NightseamDuplex"])
    ]
)
