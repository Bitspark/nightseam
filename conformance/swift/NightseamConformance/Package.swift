// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "NightseamConformance",
    platforms: [.macOS(.v13)],
    products: [.executable(name: "nightseam-swift-testee", targets: ["NightseamTestee"])],
    dependencies: [.package(url: "https://github.com/Bitspark/bitwire.git", exact: "0.2.0"),
        .package(name: "NightseamRuntime", path: "../../../runtime/swift/NightseamRuntime"),
        .package(name: "NightseamDuplex", path: "../../../duplex/swift/NightseamDuplex")
    ],
    targets: [.executableTarget(name: "NightseamTestee", dependencies: [.product(name: "Bitwire", package: "bitwire"),
        .product(name: "NightseamRuntime", package: "NightseamRuntime"),
        .product(name: "NightseamDuplex", package: "NightseamDuplex")
    ])],
    swiftLanguageModes: [.v6]
)
