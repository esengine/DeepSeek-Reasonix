import XCTest
import SwiftTreeSitter
import TreeSitterJSON

final class TreeSitterJSONTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_json())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading JSON grammar")
    }
}
