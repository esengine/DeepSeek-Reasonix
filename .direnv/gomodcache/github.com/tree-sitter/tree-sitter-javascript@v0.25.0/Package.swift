// swift-tools-version:5.3

import Foundation
import PackageDescription

var sources = ["src/parser.c"]
if FileManager.default.fileExists(atPath: "src/scanner.c") {
    sources.append("src/scanner.c")
}

let package = Package(
    name: "TreeSitterJavaScript",
    products: [
        .library(name: "TreeSitterJavaScript", targets: ["TreeSitterJavaScript"]),
    ],
    dependencies: [
        .package(name: "SwiftTreeSitter", url: "https://github.com/tree-sitter/swift-tree-sitter", from: "0.9.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterJavaScript",
            dependencies: [],
            path: ".",
            sources: sources,
            resources: [
                .copy("queries")
            ],
            publicHeadersPath: "bindings/swift",
            cSettings: [.headerSearchPath("src")]
        ),
        .testTarget(
            name: "TreeSitterJavaScriptTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterJavaScript",
            ],
            path: "bindings/swift/TreeSitterJavaScriptTests"
        )
    ],
    cLanguageStandard: .c11
)
