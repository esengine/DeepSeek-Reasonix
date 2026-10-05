//go:build treesitter && cgo

package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type codeIndexTreeSitterSpec struct {
	language func() unsafe.Pointer
	query    string
}

func (c codeIndex) parseTreeSitter(path string) ([]codeSymbol, bool, error) {
	spec, ok := codeIndexTreeSitterSpecForExt(filepath.Ext(path))
	if !ok {
		return nil, false, nil
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	language := sitter.NewLanguage(spec.language())
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		return nil, true, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, true, fmt.Errorf("tree-sitter parse returned nil")
	}
	defer tree.Close()
	query, qerr := sitter.NewQuery(language, spec.query)
	if qerr != nil {
		return nil, true, qerr
	}
	defer query.Close()

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.SetTimeoutMicros(50_000)

	captureNames := query.CaptureNames()
	matches := cursor.Matches(query, tree.RootNode(), source)
	var out []codeSymbol
	for match := matches.Next(); match != nil; match = matches.Next() {
		var nameNode *sitter.Node
		var symbolNode *sitter.Node
		var captureKind string
		for _, capture := range match.Captures {
			captureName := captureNames[capture.Index]
			kind, role, ok := splitCodeIndexTreeSitterCapture(captureName)
			if !ok {
				continue
			}
			switch role {
			case "name":
				captureKind = kind
				node := capture.Node
				nameNode = &node
			case "symbol":
				if captureKind == "" {
					captureKind = kind
				}
				node := capture.Node
				symbolNode = &node
			}
		}
		if nameNode == nil || captureKind == "" {
			continue
		}
		if symbolNode == nil {
			symbolNode = nameNode
		}
		name := strings.TrimSpace(nameNode.Utf8Text(source))
		if name == "" {
			continue
		}
		kind := normalizeCodeIndexKind(captureKind)
		parent := treeSitterParentName(kind, symbolNode, source)
		out = append(out, codeSymbol{
			Name:      name,
			Kind:      kind,
			File:      c.displayPath(path),
			Line:      int(symbolNode.StartPosition().Row) + 1,
			Parent:    parent,
			Signature: treeSitterLine(source, int(symbolNode.StartByte())),
		})
	}
	if cursor.DidExceedMatchLimit() {
		return out, true, fmt.Errorf("tree-sitter query exceeded match limit")
	}
	return out, true, nil
}

func codeIndexTreeSitterEnabled() bool {
	return true
}

func codeIndexTreeSitterSpecForExt(ext string) (codeIndexTreeSitterSpec, bool) {
	switch ext {
	case ".js", ".jsx":
		return codeIndexTreeSitterSpec{language: tree_sitter_javascript.Language, query: treeSitterJavaScriptQuery}, true
	case ".ts":
		return codeIndexTreeSitterSpec{language: tree_sitter_typescript.LanguageTypescript, query: treeSitterTypeScriptQuery}, true
	case ".tsx":
		return codeIndexTreeSitterSpec{language: tree_sitter_typescript.LanguageTSX, query: treeSitterTypeScriptQuery}, true
	case ".py":
		return codeIndexTreeSitterSpec{language: tree_sitter_python.Language, query: treeSitterPythonQuery}, true
	case ".rs":
		return codeIndexTreeSitterSpec{language: tree_sitter_rust.Language, query: treeSitterRustQuery}, true
	default:
		return codeIndexTreeSitterSpec{}, false
	}
}

func splitCodeIndexTreeSitterCapture(name string) (kind, role string, ok bool) {
	before, after, found := strings.Cut(name, ".")
	if !found || before == "" || after == "" {
		return "", "", false
	}
	if after != "name" && after != "symbol" {
		return "", "", false
	}
	return before, after, true
}

func treeSitterParentName(kind string, node *sitter.Node, source []byte) string {
	if kind != "method" {
		return ""
	}
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Kind() {
		case "abstract_class_declaration", "class_declaration", "class":
			if name := parent.ChildByFieldName("name"); name != nil {
				return strings.TrimSpace(name.Utf8Text(source))
			}
		}
	}
	return ""
}

func treeSitterLine(source []byte, start int) string {
	if start < 0 {
		start = 0
	}
	if start > len(source) {
		start = len(source)
	}
	lineStart := start
	for lineStart > 0 && source[lineStart-1] != '\n' && source[lineStart-1] != '\r' {
		lineStart--
	}
	lineEnd := start
	for lineEnd < len(source) && source[lineEnd] != '\n' && source[lineEnd] != '\r' {
		lineEnd++
	}
	return strings.TrimSpace(string(source[lineStart:lineEnd]))
}

const treeSitterJavaScriptQuery = `
(function_declaration
  name: (identifier) @func.name) @func.symbol
(generator_function_declaration
  name: (identifier) @func.name) @func.symbol
(class_declaration
  name: (identifier) @class.name) @class.symbol
(method_definition
  name: [(property_identifier) (private_property_identifier)] @method.name) @method.symbol
(lexical_declaration
  (variable_declarator
    name: (identifier) @func.name
    value: [(arrow_function) (function_expression)])) @func.symbol
(variable_declaration
  (variable_declarator
    name: (identifier) @func.name
    value: [(arrow_function) (function_expression)])) @func.symbol
`

const treeSitterTypeScriptQuery = `
(function_declaration
  name: (identifier) @func.name) @func.symbol
(generator_function_declaration
  name: (identifier) @func.name) @func.symbol
(class_declaration
  name: (type_identifier) @class.name) @class.symbol
(abstract_class_declaration
  name: (type_identifier) @class.name) @class.symbol
(method_definition
  name: [(property_identifier) (private_property_identifier)] @method.name) @method.symbol
(interface_declaration
  name: (type_identifier) @interface.name) @interface.symbol
(type_alias_declaration
  name: (type_identifier) @type.name) @type.symbol
(enum_declaration
  name: (identifier) @enum.name) @enum.symbol
(lexical_declaration
  (variable_declarator
    name: (identifier) @func.name
    value: [(arrow_function) (function_expression)])) @func.symbol
(variable_declaration
  (variable_declarator
    name: (identifier) @func.name
    value: [(arrow_function) (function_expression)])) @func.symbol
`

const treeSitterPythonQuery = `
(function_definition
  name: (identifier) @func.name) @func.symbol
(class_definition
  name: (identifier) @class.name) @class.symbol
`

const treeSitterRustQuery = `
(function_item
  name: (identifier) @fn.name) @fn.symbol
(struct_item
  name: (type_identifier) @struct.name) @struct.symbol
(enum_item
  name: (type_identifier) @enum.name) @enum.symbol
(trait_item
  name: (type_identifier) @trait.name) @trait.symbol
`
