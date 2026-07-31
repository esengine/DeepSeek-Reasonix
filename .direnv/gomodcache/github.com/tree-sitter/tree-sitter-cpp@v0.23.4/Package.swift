// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterCPP",
    products: [
        .library(name: "TreeSitterCPP", targets: ["TreeSitterCPP"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.8.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterCPP",
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
            name: "TreeSitterCPPTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterCPP",
            ],
            path: "bindings/swift/TreeSitterCPPTests"
        )
    ],
    cLanguageStandard: .c11
)
