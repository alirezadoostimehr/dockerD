// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "DockerD",
    platforms: [.macOS(.v14)],
    products: [.executable(name: "DockerD", targets: ["DockerD"])],
    targets: [
        .executableTarget(name: "DockerD"),
        .testTarget(name: "DockerDTests", dependencies: ["DockerD"])
    ]
)
