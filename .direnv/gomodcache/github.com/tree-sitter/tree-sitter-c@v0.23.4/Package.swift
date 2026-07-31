// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterC",
    products: [
        .library(name: "TreeSitterC", targets: ["TreeSitterC"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.8.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterC",
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
            name: "TreeSitterCTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterC",
            ],
            path: "bindings/swift/TreeSitterCTests"
        )
    ],
    cLanguageStandard: .c11
)
