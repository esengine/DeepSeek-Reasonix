import XCTest
import SwiftTreeSitter
import TreeSitterPython

final class TreeSitterPythonTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_python())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading Python grammar")
    }
}
