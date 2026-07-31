// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterHTML",
    products: [
        .library(name: "TreeSitterHTML", targets: ["TreeSitterHTML"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.8.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterHTML",
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
            name: "TreeSitterHTMLTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterHTML",
            ],
            path: "bindings/swift/TreeSitterHTMLTests"
        )
    ],
    cLanguageStandard: .c11
)
