import XCTest
import SwiftTreeSitter
import TreeSitterCPP

final class TreeSitterCPPTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_cpp())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading C++ grammar")
    }
}
