package tree_sitter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSymbolMetadataChecks(t *testing.T) {
	language := getLanguage("rust")
	for id := range language.NodeKindCount() {
		name := language.NodeKindForId(uint16(id))

		switch name {
		case "_type", "_expression", "_pattern", "_literal", "_literal_pattern", "_declaration_statement":
			assert.True(t, language.NodeKindIsSupertype(uint16(id)))

		case "_raw_string_literal_start", "_raw_string_literal_end", "_line_doc_comment", "_error_sentinel":
			assert.False(t, language.NodeKindIsSupertype(uint16(id)))

		case "enum_item", "struct_item", "type_item":
			assert.True(t, language.NodeKindIsNamed(uint16(id)))

		case "=>", "[", "]", "(", ")", "{", "}":
			assert.True(t, language.NodeKindIsVisible(uint16(id)))
		}
	}
}
