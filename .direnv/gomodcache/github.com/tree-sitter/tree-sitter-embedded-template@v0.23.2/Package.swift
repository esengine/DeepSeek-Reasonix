// swift-tools-version:5.3
import PackageDescription

let package = Package(
    name: "TreeSitterEmbeddedTemplate",
    products: [
        .library(name: "TreeSitterEmbeddedTemplate", targets: ["TreeSitterEmbeddedTemplate"]),
    ],
    dependencies: [
        .package(url: "https://github.com/ChimeHQ/SwiftTreeSitter", from: "0.8.0"),
    ],
    targets: [
        .target(
            name: "TreeSitterEmbeddedTemplate",
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
            name: "TreeSitterEmbeddedTemplateTests",
            dependencies: [
                "SwiftTreeSitter",
                "TreeSitterEmbeddedTemplate",
            ],
            path: "bindings/swift/TreeSitterEmbeddedTemplateTests"
        )
    ],
    cLanguageStandard: .c11
)
