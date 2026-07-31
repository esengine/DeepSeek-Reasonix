package tree_sitter_typescript_test

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func TestCanLoadTypescriptGrammar(t *testing.T) {
	language := tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	if language == nil {
		t.Errorf("Error loading TypeScript grammar")
	}
}

func TestCanLoadTSXGrammar(t *testing.T) {
	language := tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX())
	if language == nil {
		t.Errorf("Error loading TSX grammar")
	}
}
