import XCTest
import SwiftTreeSitter
import TreeSitterRuby

final class TreeSitterRubyTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_ruby())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading Ruby grammar")
    }
}
