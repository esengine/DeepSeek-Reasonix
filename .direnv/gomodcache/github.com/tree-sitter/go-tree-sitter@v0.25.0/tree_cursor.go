package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
*/
import "C"

// A stateful object for walking a syntax [Tree] efficiently.
type TreeCursor struct {
	_inner C.TSTreeCursor
}

func newTreeCursor(node Node) *TreeCursor {
	return &TreeCursor{_inner: C.ts_tree_cursor_new(node._inner)}
}

func (tc *TreeCursor) Close() {
	C.ts_tree_cursor_delete(&tc._inner)
}

func (tc *TreeCursor) Copy() *TreeCursor {
	return &TreeCursor{_inner: C.ts_tree_cursor_copy(&tc._inner)}
}

// Get the tree cursor's current [Node].
func (tc *TreeCursor) Node() *Node {
	return newNode(C.ts_tree_cursor_current_node(&tc._inner))
}

// Get the numerical field id of this tree cursor's current node.
//
// See also [TreeCursor.FieldName].
func (tc *TreeCursor) FieldId() uint16 {
	return uint16(C.ts_tree_cursor_current_field_id(&tc._inner))
}

// Get the field name of this tree cursor's current node.
func (tc *TreeCursor) FieldName() string {
	return C.GoString(C.ts_tree_cursor_current_field_name(&tc._inner))
}

// Get the depth of the cursor's current node relative to the original
// node that the cursor was constructed with.
func (tc *TreeCursor) Depth() uint32 {
	return uint32(C.ts_tree_cursor_current_depth(&tc._inner))
}

// Get the index of the cursor's current node out of all of the
// descendants of the original node that the cursor was constructed with.
func (tc *TreeCursor) DescendantIndex() uint32 {
	return uint32(C.ts_tree_cursor_current_descendant_index(&tc._inner))
}

// Move this cursor to the first child of its current node.
//
// This returns `true` if the cursor successfully moved, and returns
// `false` if there were no children.
func (tc *TreeCursor) GotoFirstChild() bool {
	return bool(C.ts_tree_cursor_goto_first_child(&tc._inner))
}

// Move this cursor to the last child of its current node.
//
// This returns `true` if the cursor successfully moved, and returns
// `false` if there were no children.
//
// Note that this function may be slower than
// [TreeCursor.GotoFirstChild] because it needs to
// iterate through all the children to compute the child's position.
func (tc *TreeCursor) GotoLastChild() bool {
	return bool(C.ts_tree_cursor_goto_last_child(&tc._inner))
}

// Move this cursor to the parent of its current node.
//
// This returns `true` if the cursor successfully moved, and returns
// `false` if there was no parent node (the cursor was already on the
// root node).
//
// Note that the given node is considered the root of the cursor,
// and the cursor cannot walk outside this node.
func (tc *TreeCursor) GotoParent() bool {
	return bool(C.ts_tree_cursor_goto_parent(&tc._inner))
}

// Move this cursor to the next sibling of its current node.
//
// This returns `true` if the cursor successfully moved, and returns
// `false` if there was no next sibling node.
//
// Note that the given node is considered the root of the cursor,
// and the cursor cannot walk outside this node.
func (tc *TreeCursor) GotoNextSibling() bool {
	return bool(C.ts_tree_cursor_goto_next_sibling(&tc._inner))
}

// Move the cursor to the node that is the nth descendant of
// the original node that the cursor was constructed with, where
// zero represents the original node itself.
func (tc *TreeCursor) GotoDescendant(descendantIndex uint32) {
	C.ts_tree_cursor_goto_descendant(&tc._inner, C.uint32_t(descendantIndex))
}

// Move this cursor to the previous sibling of its current node.
//
// This returns `true` if the cursor successfully moved, and returns
// `false` if there was no previous sibling node.
//
// Note, that this function may be slower than
// [TreeCursor.GotoNextSibling] due to how node
// positions are stored. In the worst case, this will need to iterate
// through all the children upto the previous sibling node to recalculate
// its position. Also note that the node the cursor was constructed with
// is considered the root of the cursor, and the cursor cannot
// walk outside this node.
func (tc *TreeCursor) GotoPreviousSibling() bool {
	return bool(C.ts_tree_cursor_goto_previous_sibling(&tc._inner))
}

// Move this cursor to the first child of its current node that extends
// beyond the given byte offset.
//
// This returns the index of the child node if one was found, and returns
// `nil` if no such child was found.
func (tc *TreeCursor) GotoFirstChildForByte(byteIndex uint32) *uint {
	res := C.ts_tree_cursor_goto_first_child_for_byte(&tc._inner, C.uint32_t(byteIndex))
	if res < 0 {
		return nil
	}
	index := uint(res)
	return &index
}

// Move this cursor to the first child of its current node that extends
// beyond the given byte offset.
//
// This returns the index of the child node if one was found, and returns
// `nil` if no such child was found.
func (tc *TreeCursor) GotoFirstChildForPoint(point Point) *uint {
	res := C.ts_tree_cursor_goto_first_child_for_point(&tc._inner, point.toTSPoint())
	if res < 0 {
		return nil
	}
	index := uint(res)
	return &index
}

// Re-initialize this tree cursor to start at the original node that the
// cursor was constructed with.
func (tc *TreeCursor) Reset(node Node) {
	C.ts_tree_cursor_reset(&tc._inner, node._inner)
}

// Re-initialize a tree cursor to the same position as another cursor.
//
// Unlike [TreeCursor.Reset], this will not lose parent
// information and allows reusing already created cursors.
func (tc *TreeCursor) ResetTo(cursor *TreeCursor) {
	C.ts_tree_cursor_reset_to(&tc._inner, &cursor._inner)
}
