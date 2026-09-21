// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "NightseamConformance",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "nightseam-swift-testee", targets: ["NightseamTestee"])],
    dependencies: [
        .package(name: "NightseamRuntime", path: "../../../runtime/swift/NightseamRuntime"),
        .package(name: "NightseamDuplex", path: "../../../duplex/swift/NightseamDuplex")
    ],
    targets: [.executableTarget(name: "NightseamTestee", dependencies: [
        .product(name: "NightseamRuntime", package: "NightseamRuntime"),
        .product(name: "NightseamDuplex", package: "NightseamDuplex")
    ])],
    swiftLanguageModes: [.v6]
)
