from unittest import TestCase

import tree_sitter, tree_sitter_typescript


class TestLanguage(TestCase):
    def test_can_load_typescript_grammar(self):
        try:
            tree_sitter.Language(tree_sitter_typescript.language_typescript())
        except Exception:
            self.fail("Error loading TypeScript grammar")

    def test_can_load_tsx_grammar(self):
        try:
            tree_sitter.Language(tree_sitter_typescript.language_tsx())
        except Exception:
            self.fail("Error loading TSX grammar")
