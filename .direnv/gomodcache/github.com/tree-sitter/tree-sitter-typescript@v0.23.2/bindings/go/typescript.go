package tree_sitter_typescript

// #cgo CPPFLAGS: -I../../typescript/src
// #cgo CFLAGS: -std=c11 -fPIC
// #include "../../typescript/src/parser.c"
// #include "../../typescript/src/scanner.c"
import "C"

import "unsafe"

// Get the tree-sitter Language for TypeScript.
func LanguageTypescript() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_typescript())
}
