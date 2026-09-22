// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "NightseamDuplex",
    platforms: [.macOS(.v13), .iOS(.v16), .tvOS(.v16), .watchOS(.v9)],
    products: [.library(name: "NightseamDuplex", targets: ["NightseamDuplex"])],
    dependencies: [.package(url: "https://github.com/Bitspark/bitwire.git", exact: "0.2.0")],
    targets: [
        .target(name: "NightseamDuplex", dependencies: [.product(name: "Bitwire", package: "bitwire")]),
        .testTarget(name: "NightseamDuplexTests", dependencies: [.product(name: "Bitwire", package: "bitwire"), "NightseamDuplex"])
    ]
)
