package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
*/
import "C"

import (
	"unsafe"
)

// A stateful object that this is used to produce a [Tree] based on some
// source code.
type Tree struct {
	_inner *C.TSTree
}

// Create a new tree from a raw pointer.
func newTree(inner *C.TSTree) *Tree {
	return &Tree{_inner: inner}
}

// Get the root node of the syntax tree.
func (t *Tree) RootNode() *Node {
	return &Node{_inner: C.ts_tree_root_node(t._inner)}
}

// Get the root node of the syntax tree, but with its position shifted
// forward by the given offset.
func (t *Tree) RootNodeWithOffset(offsetBytes int, offsetExtent Point) *Node {
	return &Node{_inner: C.ts_tree_root_node_with_offset(t._inner, C.uint(offsetBytes), offsetExtent.toTSPoint())}
}

// Get the language that was used to parse the syntax tree.
func (t *Tree) Language() *Language {
	return &Language{Inner: C.ts_tree_language(t._inner)}
}

// Edit the syntax tree to keep it in sync with source code that has been
// edited.
//
// You must describe the edit both in terms of byte offsets and in terms of
// row/column coordinates.
func (t *Tree) Edit(edit *InputEdit) {
	C.ts_tree_edit(t._inner, edit.toTSInputEdit())
}

// Create a new [TreeCursor] starting from the root of the tree.
func (t *Tree) Walk() *TreeCursor {
	return t.RootNode().Walk()
}

// Compare this old edited syntax tree to a new syntax tree representing
// the same document, returning a sequence of ranges whose syntactic
// structure has changed.
//
// For this to work correctly, this syntax tree must have been edited such
// that its ranges match up to the new tree. Generally, you'll want to
// call this method right after calling one of the [Parser.parse]
// functions. Call it on the old tree that was passed to parse, and
// pass the new tree that was returned from `parse`.
//
// The returned ranges indicate areas where the hierarchical structure of syntax
// nodes (from root to leaf) has changed between the old and new trees. Characters
// outside these ranges have identical ancestor nodes in both trees.
//
// Note that the returned ranges may be slightly larger than the exact changed areas,
// but Tree-sitter attempts to make them as small as possible.
func (t *Tree) ChangedRanges(other *Tree) []Range {
	var count C.uint
	ptr := C.ts_tree_get_changed_ranges(t._inner, other._inner, &count)
	ranges := make([]Range, int(count))
	for i := uintptr(0); i < uintptr(count); i++ {
		val := *(*C.TSRange)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + i*unsafe.Sizeof(*ptr)))
		ranges[i] = Range{
			StartPoint: Point{Row: uint(val.start_point.row), Column: uint(val.start_point.column)},
			EndPoint:   Point{Row: uint(val.end_point.row), Column: uint(val.end_point.column)},
			StartByte:  uint(val.start_byte),
			EndByte:    uint(val.end_byte),
		}
	}
	go_free(unsafe.Pointer(ptr))
	return ranges
}

// Get the included ranges that were used to parse the syntax tree.
func (t *Tree) IncludedRanges() []Range {
	var count C.uint
	ptr := C.ts_tree_included_ranges(t._inner, &count)
	ranges := make([]Range, int(count))
	for i := uintptr(0); i < uintptr(count); i++ {
		val := *(*C.TSRange)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + i*unsafe.Sizeof(*ptr)))
		ranges[i] = Range{
			StartPoint: Point{Row: uint(val.start_point.row), Column: uint(val.start_point.column)},
			EndPoint:   Point{Row: uint(val.end_point.row), Column: uint(val.end_point.column)},
			StartByte:  uint(val.start_byte),
			EndByte:    uint(val.end_byte),
		}
	}
	go_free(unsafe.Pointer(ptr))
	return ranges
}

// Print a graph of the tree to the given file descriptor.
// The graph is formatted in the DOT language. You may want to pipe this
// graph directly to a `dot(1)` process in order to generate SVG
// output.
func (t *Tree) PrintDotGraph(file int) {
	C.ts_tree_print_dot_graph(t._inner, C.int(file))
}

func (t *Tree) Close() {
	if t != nil {
		C.ts_tree_delete(t._inner)
	}
}

func (t *Tree) Clone() *Tree {
	return newTree(C.ts_tree_copy(t._inner))
}
