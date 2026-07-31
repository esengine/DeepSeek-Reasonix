import XCTest
import SwiftTreeSitter
import TreeSitterJavaScript

final class TreeSitterJavaScriptTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_javascript())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading JavaScript grammar")
    }
}
