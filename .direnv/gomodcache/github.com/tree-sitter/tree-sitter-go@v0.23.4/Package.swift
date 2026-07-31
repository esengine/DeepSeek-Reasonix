// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterGo",
    products: [
        .library(name: "TreeSitterGo", targets: ["TreeSitterGo"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.8.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterGo",
            dependencies: [],
            path: ".",
            sources: [
                "src/parser.c",
            ],
            resources: [
                .copy("queries")
            ],
            publicHeadersPath: "bindings/swift",
            cSettings: [.headerSearchPath("src")]
        ),
        .testTarget(
            name: "TreeSitterGoTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterGo",
            ],
            path: "bindings/swift/TreeSitterGoTests"
        )
    ],
    cLanguageStandard: .c11
)
