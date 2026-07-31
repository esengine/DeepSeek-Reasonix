//! This crate provides PHP language support for the [tree-sitter][] parsing library.
//!
//! Typically, you will use the [LANGUAGE_PHP][] constant to add this language to a
//! tree-sitter [Parser][], and then use the parser to parse some code:
//!
//! ```
//! use tree_sitter::Parser;
//!
//! let code = r#"
//! <?php
//!   echo "Hello, World!";
//! ?>
//! "#;
//! let mut parser = Parser::new();
//! let language = tree_sitter_php::LANGUAGE_PHP;
//! parser
//!     .set_language(&language.into())
//!     .expect("Error loading PHP parser");
//! let tree = parser.parse(code, None).unwrap();
//! assert!(!tree.root_node().has_error());
//! ```
//!
//! [Parser]: https://docs.rs/tree-sitter/*/tree_sitter/struct.Parser.html
//! [tree-sitter]: https://tree-sitter.github.io/

use tree_sitter_language::LanguageFn;

extern "C" {
    fn tree_sitter_php() -> *const ();
    fn tree_sitter_php_only() -> *const ();
}

/// The tree-sitter [`LanguageFn`] for PHP.
///
/// [LanguageFn]: https://docs.rs/tree-sitter-language/*/tree_sitter_language/struct.LanguageFn.html
pub const LANGUAGE_PHP: LanguageFn = unsafe { LanguageFn::from_raw(tree_sitter_php) };

/// The tree-sitter [`LanguageFn`] for PHP-Only.
///
/// [LanguageFn]: https://docs.rs/tree-sitter-language/*/tree_sitter_language/struct.LanguageFn.html
pub const LANGUAGE_PHP_ONLY: LanguageFn = unsafe { LanguageFn::from_raw(tree_sitter_php_only) };

/// The content of the [`node-types.json`][] file for this grammar.
///
/// [`node-types.json`]: https://tree-sitter.github.io/tree-sitter/using-parsers#static-node-types
pub const PHP_NODE_TYPES: &str = include_str!("../../php/src/node-types.json");
pub const PHP_ONLY_NODE_TYPES: &str = include_str!("../../php_only/src/node-types.json");

/// The syntax highlighting query for PHP.
pub const HIGHLIGHTS_QUERY: &str = include_str!("../../queries/highlights.scm");

/// The injection query for PHP.
pub const INJECTIONS_QUERY: &str = include_str!("../../queries/injections.scm");

/// The symbol tagging query for PHP.
pub const TAGS_QUERY: &str = include_str!("../../queries/tags.scm");

#[cfg(test)]
mod tests {
    #[test]
    fn test_can_load_php_grammar() {
        let mut parser = tree_sitter::Parser::new();
        parser
            .set_language(&super::LANGUAGE_PHP.into())
            .expect("Error loading PHP parser");
    }

    #[test]
    fn test_can_load_php_only_grammar() {
        let mut parser = tree_sitter::Parser::new();
        parser
            .set_language(&super::LANGUAGE_PHP_ONLY.into())
            .expect("Error loading PHP-Only parser");
    }
}
