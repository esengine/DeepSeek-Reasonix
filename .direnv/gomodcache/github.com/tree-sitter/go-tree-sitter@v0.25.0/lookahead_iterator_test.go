package tree_sitter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	. "github.com/tree-sitter/go-tree-sitter"
)

func TestLookaheadIterator(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	language := getLanguage("rust")
	parser.SetLanguage(language)

	tree := parser.Parse([]byte("struct Stuff {}"), nil)
	defer tree.Close()
	assert.NotNil(t, tree)

	cursor := tree.Walk()

	assert.True(t, cursor.GotoFirstChild()) // struct
	assert.True(t, cursor.GotoFirstChild()) // struct keyword

	nextState := cursor.Node().NextParseState()
	assert.NotEqual(t, 0, nextState)
	assert.Equal(t, nextState, language.NextState(cursor.Node().ParseState(), cursor.Node().GrammarId()))
	assert.True(t, uint(nextState) < uint(language.ParseStateCount()))
	assert.True(t, cursor.GotoNextSibling()) // type_identifier
	assert.Equal(t, nextState, cursor.Node().ParseState())
	assert.Equal(t, cursor.Node().GrammarName(), "identifier")
	assert.NotEqual(t, cursor.Node().GrammarId(), cursor.Node().KindId())

	expectedSymbols := []string{"//", "/*", "identifier", "line_comment", "block_comment"}
	lookahead := language.LookaheadIterator(nextState)
	defer lookahead.Close()
	assert.NotNil(t, lookahead)
	assert.Equal(t, lookahead.Language(), language)
	assert.Equal(t, lookahead.IterNames(), expectedSymbols)

	lookahead.ResetState(nextState)
	assert.Equal(t, lookahead.IterNames(), expectedSymbols)

	lookahead.Reset(language, nextState)
	var names []string
	symbols := lookahead.Iter()
	for _, s := range symbols {
		names = append(names, language.NodeKindForId(s))
	}
	assert.Equal(t, names, expectedSymbols)
}
