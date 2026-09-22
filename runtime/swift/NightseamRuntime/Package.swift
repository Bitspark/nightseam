// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "NightseamRuntime",
    platforms: [.macOS(.v13), .iOS(.v16), .tvOS(.v16), .watchOS(.v9)],
    products: [.library(name: "NightseamRuntime", targets: ["NightseamRuntime"])],
    dependencies: [.package(url: "https://github.com/Bitspark/bitwire.git", exact: "0.2.0"), .package(name: "NightseamDuplex", path: "../../../duplex/swift/NightseamDuplex")],
    targets: [
        .target(name: "NightseamRuntime", dependencies: [.product(name: "Bitwire", package: "bitwire"), .product(name: "NightseamDuplex", package: "NightseamDuplex")]),
        .testTarget(name: "NightseamRuntimeTests", dependencies: [.product(name: "Bitwire", package: "bitwire"), "NightseamRuntime"])
    ],
    swiftLanguageModes: [.v6]
)
