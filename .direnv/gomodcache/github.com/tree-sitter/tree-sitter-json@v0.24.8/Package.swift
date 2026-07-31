// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterJSON",
    products: [
        .library(name: "TreeSitterJSON", targets: ["TreeSitterJSON"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.8.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterJSON",
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
            name: "TreeSitterJSONTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterJSON",
            ],
            path: "bindings/swift/TreeSitterJSONTests"
        )
    ],
    cLanguageStandard: .c11
)
