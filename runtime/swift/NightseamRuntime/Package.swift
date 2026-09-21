// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "NightseamRuntime",
    platforms: [.macOS(.v13), .iOS(.v16), .tvOS(.v16), .watchOS(.v9)],
    products: [.library(name: "NightseamRuntime", targets: ["NightseamRuntime"])],
    dependencies: [.package(name: "NightseamDuplex", path: "../../../duplex/swift/NightseamDuplex")],
    targets: [
        .target(name: "NightseamRuntime", dependencies: [.product(name: "NightseamDuplex", package: "NightseamDuplex")]),
        .testTarget(name: "NightseamRuntimeTests", dependencies: ["NightseamRuntime"])
    ],
    swiftLanguageModes: [.v6]
)
