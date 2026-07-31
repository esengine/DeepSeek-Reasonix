
import XCTest
import SwiftTreeSitter
import TreeSitterPHP

final class TreeSitterPHPTests: XCTestCase {
    func testCanLoaPHPGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_php())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading PHP grammar")
    }

    func testCanLoadPHPOnlyGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_php_only())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading PHP-Only grammar")
    }
}

