package tree_sitter_test

import (
	"fmt"

	. "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func ExampleTreeCursor() {
	parser := NewParser()
	defer parser.Close()

	language := NewLanguage(tree_sitter_go.Language())

	parser.SetLanguage(language)

	tree := parser.Parse(
		[]byte(`
			package main


			func main() {
				return
			}
		`),
		nil,
	)
	defer tree.Close()

	cursor := tree.Walk()
	defer cursor.Close()

	fmt.Println(cursor.Node().Kind())

	fmt.Println(cursor.GotoFirstChild())
	fmt.Println(cursor.Node().Kind())

	fmt.Println(cursor.GotoFirstChild())
	fmt.Println(cursor.Node().Kind())

	// Returns `false` because the `package` node has no children
	fmt.Println(cursor.GotoFirstChild())

	fmt.Println(cursor.GotoNextSibling())
	fmt.Println(cursor.Node().Kind())

	fmt.Println(cursor.GotoParent())
	fmt.Println(cursor.Node().Kind())

	fmt.Println(cursor.GotoNextSibling())
	fmt.Println(cursor.GotoNextSibling())
	fmt.Println(cursor.Node().Kind())

	// Output:
	// source_file
	// true
	// package_clause
	// true
	// package
	// false
	// true
	// package_identifier
	// true
	// package_clause
	// true
	// false
	// function_declaration
}
