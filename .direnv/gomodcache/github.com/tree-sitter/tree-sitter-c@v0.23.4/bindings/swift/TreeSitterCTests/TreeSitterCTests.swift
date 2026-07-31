import XCTest
import SwiftTreeSitter
import TreeSitterC

final class TreeSitterCTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_c())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading C grammar")
    }
}
