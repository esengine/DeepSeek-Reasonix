import XCTest
import SwiftTreeSitter
import TreeSitterRust

final class TreeSitterRustTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_rust())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading Rust grammar")
    }
}
