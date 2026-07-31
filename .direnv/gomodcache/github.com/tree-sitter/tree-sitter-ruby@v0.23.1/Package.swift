// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterRuby",
    products: [
        .library(name: "TreeSitterRuby", targets: ["TreeSitterRuby"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.8.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterRuby",
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
            name: "TreeSitterRubyTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterRuby",
            ],
            path: "bindings/swift/TreeSitterRubyTests"
        )
    ],
    cLanguageStandard: .c11
)
