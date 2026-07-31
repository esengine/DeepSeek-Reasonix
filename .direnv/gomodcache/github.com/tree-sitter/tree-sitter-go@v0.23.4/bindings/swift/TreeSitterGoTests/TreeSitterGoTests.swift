import XCTest
import SwiftTreeSitter
import TreeSitterGo

final class TreeSitterGoTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_go())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading Go grammar")
    }
}
