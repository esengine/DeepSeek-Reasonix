package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
*/
import "C"
import "unsafe"

// A single node within a syntax [Tree].
// Note that this is a C-compatible struct
type Node struct {
	_inner C.TSNode
}

func newNode(node C.TSNode) *Node {
	if node.id == nil {
		return nil
	}
	return &Node{_inner: node}
}

// Get a numeric id for this node that is unique.
//
// Within a given syntax tree, no two nodes have the same id. However, if
// a new tree is created based on an older tree, and a node from the old
// tree is reused in the process, then that node will have the same id in
// both trees.
func (n *Node) Id() uintptr {
	return uintptr(n._inner.id)
}

// Get this node's type as a numerical id.
func (n *Node) KindId() uint16 {
	return uint16(C.ts_node_symbol(n._inner))
}

// Get the node's type as a numerical id as it appears in the grammar
// ignoring aliases.
func (n *Node) GrammarId() uint16 {
	return uint16(C.ts_node_grammar_symbol(n._inner))
}

// Get this node's type as a string.
func (n *Node) Kind() string {
	return C.GoString(C.ts_node_type(n._inner))
}

// Get this node's symbol name as it appears in the grammar ignoring
// aliases as a string.
func (n *Node) GrammarName() string {
	return C.GoString(C.ts_node_grammar_type(n._inner))
}

// Get the [Language] that was used to parse this node's syntax tree.
func (n *Node) Language() *Language {
	return &Language{Inner: C.ts_node_language(n._inner)}
}

// Check if this node is *named*.
//
// Named nodes correspond to named rules in the grammar, whereas
// *anonymous* nodes correspond to string literals in the grammar.
func (n *Node) IsNamed() bool {
	return bool(C.ts_node_is_named(n._inner))
}

// Check if this node is *extra*.
//
// Extra nodes represent things like comments, which are not required in the
// grammar, but can appear anywhere.
func (n *Node) IsExtra() bool {
	return bool(C.ts_node_is_extra(n._inner))
}

// Check if this node has been edited.
func (n *Node) HasChanges() bool {
	return bool(C.ts_node_has_changes(n._inner))
}

// Check if this node represents a syntax error or contains any syntax
// errors anywhere within it.
func (n *Node) HasError() bool {
	return bool(C.ts_node_has_error(n._inner))
}

// Check if this node represents a syntax error.
//
// Syntax errors represent parts of the code that could not be incorporated
// into a valid syntax tree.
func (n *Node) IsError() bool {
	return bool(C.ts_node_is_error(n._inner))
}

// Get this node's parse state.
func (n *Node) ParseState() uint16 {
	return uint16(C.ts_node_parse_state(n._inner))
}

// Get the parse state after this node.
func (n *Node) NextParseState() uint16 {
	return uint16(C.ts_node_next_parse_state(n._inner))
}

// Check if this node is *missing*.
//
// Missing nodes are inserted by the parser in order to recover from
// certain kinds of syntax errors.
func (n *Node) IsMissing() bool {
	return bool(C.ts_node_is_missing(n._inner))
}

// Get the byte offsets where this node starts.
func (n *Node) StartByte() uint {
	return uint(C.ts_node_start_byte(n._inner))
}

// Get the byte offsets where this node end.
func (n *Node) EndByte() uint {
	return uint(C.ts_node_end_byte(n._inner))
}

// Get the byte range of source code that this node represents.
func (n *Node) ByteRange() (uint, uint) {
	return n.StartByte(), n.EndByte()
}

// Get the range of source code that this node represents, both in terms of
// raw bytes and of row/column coordinates.
func (n *Node) Range() Range {
	return Range{
		StartByte:  n.StartByte(),
		EndByte:    n.EndByte(),
		StartPoint: n.StartPosition(),
		EndPoint:   n.EndPosition(),
	}
}

// Get this node's start position in terms of rows and columns.
func (n *Node) StartPosition() Point {
	p := Point{}
	p.fromTSPoint(C.ts_node_start_point(n._inner))
	return p
}

// Get this node's end position in terms of rows and columns.
func (n *Node) EndPosition() Point {
	p := Point{}
	p.fromTSPoint(C.ts_node_end_point(n._inner))
	return p
}

// Get the node's child at the given index, where zero represents the first
// child.
//
// This method is fairly fast, but its cost is technically log(i), so if
// you might be iterating over a long list of children, you should use
// [Node.Children] instead.
func (n *Node) Child(i uint) *Node {
	return newNode(C.ts_node_child(n._inner, C.uint(i)))
}

// Get this node's number of children.
func (n *Node) ChildCount() uint {
	return uint(C.ts_node_child_count(n._inner))
}

// Get this node's *named* child at the given index.
//
// See also [Node.IsNamed].
// This method is fairly fast, but its cost is technically log(i), so if
// you might be iterating over a long list of children, you should use
// [Node.NamedChildren] instead.
func (n *Node) NamedChild(i uint) *Node {
	return newNode(C.ts_node_named_child(n._inner, C.uint(i)))
}

// Get this node's number of *named* children.
//
// See also [Node.IsNamed].
func (n *Node) NamedChildCount() uint {
	return uint(C.ts_node_named_child_count(n._inner))
}

// Get the first child with the given field name.
//
// If multiple children may have the same field name, access them using
// [Node.ChildrenByFieldName]
func (n *Node) ChildByFieldName(fieldName string) *Node {
	cFieldName := C.CString(fieldName)
	defer go_free(unsafe.Pointer(cFieldName))
	return newNode(C.ts_node_child_by_field_name(n._inner, cFieldName, C.uint32_t(len(fieldName))))
}

// Get this node's child with the given numerical field id.
//
// See also [Node.ChildByFieldName]. You can
// convert a field name to an id using [Language.FieldIdForName].
func (n *Node) ChildByFieldId(fieldId uint16) *Node {
	return newNode(C.ts_node_child_by_field_id(n._inner, C.uint16_t(fieldId)))
}

// Get the field name of this node's child at the given index.
func (n *Node) FieldNameForChild(childIndex uint32) string {
	ptr := C.ts_node_field_name_for_child(n._inner, C.uint32_t(childIndex))
	if ptr == nil {
		return ""
	}
	return C.GoString(ptr)
}

// Get the field name of this node's named child at the given index.
func (n *Node) FieldNameForNamedChild(namedChildIndex uint32) string {
	ptr := C.ts_node_field_name_for_named_child(n._inner, C.uint32_t(namedChildIndex))
	if ptr == nil {
		return ""
	}
	return C.GoString(ptr)
}

// Iterate over this node's children.
//
// A [TreeCursor] is used to retrieve the children efficiently. Obtain
// a [TreeCursor] by calling [Tree.Walk] or [Node.Walk]. To avoid
// unnecessary allocations, you should reuse the same cursor for
// subsequent calls to this method.
//
// If you're walking the tree recursively, you may want to use the
// [TreeCursor] APIs directly instead.
func (n *Node) Children(cursor *TreeCursor) []Node {
	cursor.Reset(*n)
	cursor.GotoFirstChild()
	childCount := n.ChildCount()
	result := make([]Node, 0, childCount)
	for i := 0; i < int(childCount); i++ {
		result = append(result, *cursor.Node())
		cursor.GotoNextSibling()
	}
	return result
}

// Iterate over this node's named children.
//
// See also [Node.Children].
func (n *Node) NamedChildren(cursor *TreeCursor) []Node {
	cursor.Reset(*n)
	cursor.GotoFirstChild()
	namedChildCount := n.NamedChildCount()
	result := make([]Node, 0, namedChildCount)
	for i := 0; i < int(namedChildCount); i++ {
		for !cursor.Node().IsNamed() {
			if !cursor.GotoNextSibling() {
				break
			}
		}
		result = append(result, *cursor.Node())
		cursor.GotoNextSibling()
	}
	return result
}

// Iterate over this node's children with a given field name.
//
// See also [Node.Children].
func (n *Node) ChildrenByFieldName(fieldName string, cursor *TreeCursor) []Node {
	fieldId := n.Language().FieldIdForName(fieldName)
	done := fieldId == 0
	if !done {
		cursor.Reset(*n)
		cursor.GotoFirstChild()
	}
	result := make([]Node, 0)
	for !done {
		for cursor.FieldId() != fieldId {
			if !cursor.GotoNextSibling() {
				return result
			}
		}
		result = append(result, *cursor.Node())
		if !cursor.GotoNextSibling() {
			done = true
		}
	}
	return result
}

// Get this node's immediate parent.
// Prefer [Node.ChildWithDescendant]
// for iterating over this node's ancestors.
func (n *Node) Parent() *Node {
	return newNode(C.ts_node_parent(n._inner))
}

// Get the node that contains `descendant`.
// Note that this can return `descendant` itself.
func (n *Node) ChildWithDescendant(descendant *Node) *Node {
	return newNode(C.ts_node_child_with_descendant(n._inner, descendant._inner))
}

// Get this node's next sibling.
func (n *Node) NextSibling() *Node {
	return newNode(C.ts_node_next_sibling(n._inner))
}

// Get this node's previous sibling.
func (n *Node) PrevSibling() *Node {
	return newNode(C.ts_node_prev_sibling(n._inner))
}

// Get this node's next named sibling.
func (n *Node) NextNamedSibling() *Node {
	return newNode(C.ts_node_next_named_sibling(n._inner))
}

// Get this node's previous named sibling.
func (n *Node) PrevNamedSibling() *Node {
	return newNode(C.ts_node_prev_named_sibling(n._inner))
}

// Get the node's first child that contains or starts after the given byte offset.
func (n *Node) FirstChildForByte(byteOffset uint) *Node {
	return newNode(C.ts_node_first_child_for_byte(n._inner, C.uint(byteOffset)))
}

// Get the node's first named child that contains or starts after the given byte offset.
func (n *Node) FirstNamedChildForByte(byteOffset uint) *Node {
	return newNode(C.ts_node_first_named_child_for_byte(n._inner, C.uint(byteOffset)))
}

// Get the node's number of descendants, including one for the node itself.
func (n *Node) DescendantCount() uint {
	return uint(C.ts_node_descendant_count(n._inner))
}

// Get the smallest node within this node that spans the given range.
func (n *Node) DescendantForByteRange(start, end uint) *Node {
	return newNode(C.ts_node_descendant_for_byte_range(n._inner, C.uint(start), C.uint(end)))
}

// Get the smallest named node within this node that spans the given range.
func (n *Node) NamedDescendantForByteRange(start, end uint) *Node {
	return newNode(C.ts_node_named_descendant_for_byte_range(n._inner, C.uint(start), C.uint(end)))
}

// Get the smallest node within this node that spans the given range.
func (n *Node) DescendantForPointRange(start, end Point) *Node {
	return newNode(C.ts_node_descendant_for_point_range(n._inner, start.toTSPoint(), end.toTSPoint()))
}

// Get the smallest named node within this node that spans the given range.
func (n *Node) NamedDescendantForPointRange(start, end Point) *Node {
	return newNode(C.ts_node_named_descendant_for_point_range(n._inner, start.toTSPoint(), end.toTSPoint()))
}

func (n *Node) ToSexp() string {
	cString := C.ts_node_string(n._inner)
	result := C.GoString(cString)
	go_free(unsafe.Pointer(cString))
	return result
}

func (n *Node) Utf8Text(source []byte) string {
	return string(source[n.StartByte():n.EndByte()])
}

func (n *Node) Utf16Text(source []uint16) []uint16 {
	return source[n.StartByte():n.EndByte()]
}

// Create a new [TreeCursor] starting from this node.
//
// Note that the given node is considered the root of the cursor,
// and the cursor cannot walk outside this node.
func (n *Node) Walk() *TreeCursor {
	return newTreeCursor(*n)
}

// Edit this node to keep it in-sync with source code that has been edited.
//
// This function is only rarely needed. When you edit a syntax tree with
// the [Tree.Edit] method, all of the nodes that you retrieve from
// the tree afterward will already reflect the edit. You only need to
// use [Node.Edit] when you have a specific [Node] instance that
// you want to keep and continue to use after an edit.
func (n *Node) Edit(edit *InputEdit) {
	C.ts_node_edit(&n._inner, edit.toTSInputEdit())
}

// Check if two nodes are identical.
func (n *Node) Equals(other Node) bool {
	return bool(C.ts_node_eq(n._inner, other._inner))
}
