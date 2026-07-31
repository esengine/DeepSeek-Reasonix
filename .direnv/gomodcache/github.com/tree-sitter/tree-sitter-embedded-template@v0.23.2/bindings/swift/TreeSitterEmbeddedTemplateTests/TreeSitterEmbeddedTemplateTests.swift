import XCTest
import SwiftTreeSitter
import TreeSitterEmbeddedTemplate

final class TreeSitterEmbeddedTemplateTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_embedded_template())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading EmbeddedTemplate grammar")
    }
}
