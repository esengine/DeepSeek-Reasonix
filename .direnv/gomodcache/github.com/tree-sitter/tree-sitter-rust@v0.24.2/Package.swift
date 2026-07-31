// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterRust",
    products: [
        .library(name: "TreeSitterRust", targets: ["TreeSitterRust"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.9.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterRust",
            dependencies: [],
            path: ".",
            sources: [
                "src/parser.c",
                "src/scanner.c",
            ],
            resources: [
                .copy("queries")
            ],
            publicHeadersPath: "bindings/swift",
            cSettings: [.headerSearchPath("src")]
        ),
        .testTarget(
            name: "TreeSitterRustTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterRust",
            ],
            path: "bindings/swift/TreeSitterRustTests"
        )
    ],
    cLanguageStandard: .c11
)
