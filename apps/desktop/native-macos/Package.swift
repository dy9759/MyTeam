// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "MyTeamNative",
    platforms: [
        .macOS(.v13)
    ],
    products: [
        .executable(name: "MyTeamNative", targets: ["MyTeamNative"])
    ],
    targets: [
        .executableTarget(
            name: "MyTeamNative",
            path: "Sources/MyTeamNative"
        )
    ]
)
