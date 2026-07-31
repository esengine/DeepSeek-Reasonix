import XCTest
import SwiftTreeSitter
import TreeSitterHTML

final class TreeSitterHTMLTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_html())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading HTML grammar")
    }
}
