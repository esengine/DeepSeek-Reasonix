package tree_sitter_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	. "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func ExampleTree() {
	parser := NewParser()
	defer parser.Close()

	language := NewLanguage(tree_sitter_go.Language())

	parser.SetLanguage(language)

	source := []byte(`
			package main


			func main() {
				return
			}
	`)

	tree := parser.Parse(source, nil)
	defer tree.Close()

	// We change the return statement to `return 1`

	newSource := append([]byte(nil), source[:46]...)
	newSource = append(newSource, []byte(" 1")...)
	newSource = append(newSource, source[46:]...)

	edit := &InputEdit{
		StartByte:      46,
		OldEndByte:     46,
		NewEndByte:     46 + 2,
		StartPosition:  Point{Row: 5, Column: 9},
		OldEndPosition: Point{Row: 5, Column: 9},
		NewEndPosition: Point{Row: 5, Column: 9 + 2},
	}

	tree.Edit(edit)

	newTree := parser.Parse(newSource, tree)
	defer newTree.Close()

	for _, changedRange := range tree.ChangedRanges(newTree) {
		fmt.Println("Changed range:")
		fmt.Printf(" Start point: %v\n", changedRange.StartPoint)
		fmt.Printf(" End point: %v\n", changedRange.EndPoint)
		fmt.Printf(" Start byte: %d\n", changedRange.StartByte)
		fmt.Printf(" End byte: %d\n", changedRange.EndByte)
	}

	// Output:
	// Changed range:
	//  Start point: {5 10}
	//  End point: {5 12}
	//  Start byte: 46
	//  End byte: 48
}

func TestTreeEdit(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse([]byte("  abc  !==  def"), nil)
	defer tree.Close()

	assert.Equal(t, tree.RootNode().ToSexp(), "(program (expression_statement (binary_expression left: (identifier) right: (identifier))))")

	// edit entirely within the tree's padding:
	// resize the padding of the tree and its leftmost descendants.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      1,
			OldEndByte:     1,
			NewEndByte:     2,
			StartPosition:  Point{Row: 0, Column: 1},
			OldEndPosition: Point{Row: 0, Column: 1},
			NewEndPosition: Point{Row: 0, Column: 2},
		})

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 3)
		assert.EqualValues(t, expr.EndByte(), 16)
		assert.True(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 3)
		assert.EqualValues(t, child1.EndByte(), 6)
		assert.False(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 8)
		assert.EqualValues(t, child2.EndByte(), 11)
	}

	// edit starting in the tree's padding but extending into its content:
	// shrink the content to compensate for the expanded padding.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      1,
			OldEndByte:     4,
			NewEndByte:     5,
			StartPosition:  Point{Row: 0, Column: 1},
			OldEndPosition: Point{Row: 0, Column: 5},
			NewEndPosition: Point{Row: 0, Column: 5},
		})

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 5)
		assert.EqualValues(t, expr.EndByte(), 16)
		assert.True(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 5)
		assert.EqualValues(t, child1.EndByte(), 6)
		assert.False(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 8)
		assert.EqualValues(t, child2.EndByte(), 11)
	}

	// insertion at the edge of a tree's padding:
	// expand the tree's padding.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      2,
			OldEndByte:     2,
			NewEndByte:     4,
			StartPosition:  Point{Row: 0, Column: 2},
			OldEndPosition: Point{Row: 0, Column: 2},
			NewEndPosition: Point{Row: 0, Column: 4},
		})

		// let expr = tree.root_node().child(0).unwrap().child(0).unwrap();
		// let child1 = expr.child(0).unwrap();
		// let child2 = expr.child(1).unwrap();
		//
		// assert!(expr.has_changes());
		// assert_eq!(expr.byte_range(), 4..17);
		// assert!(child1.has_changes());
		// assert_eq!(child1.byte_range(), 4..7);
		// assert!(!child2.has_changes());
		// assert_eq!(child2.byte_range(), 9..12);

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 4)
		assert.EqualValues(t, expr.EndByte(), 17)
		assert.True(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 4)
		assert.EqualValues(t, child1.EndByte(), 7)
		assert.False(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 9)
		assert.EqualValues(t, child2.EndByte(), 12)
	}

	// replacement starting at the edge of the tree's padding:
	// resize the content and not the padding.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      2,
			OldEndByte:     2,
			NewEndByte:     4,
			StartPosition:  Point{Row: 0, Column: 2},
			OldEndPosition: Point{Row: 0, Column: 2},
			NewEndPosition: Point{Row: 0, Column: 4},
		})

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 4)
		assert.EqualValues(t, expr.EndByte(), 17)
		assert.True(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 4)
		assert.EqualValues(t, child1.EndByte(), 7)
		assert.False(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 9)
		assert.EqualValues(t, child2.EndByte(), 12)
	}

	// deletion that spans more than one child node:
	// shrink subsequent child nodes.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      1,
			OldEndByte:     11,
			NewEndByte:     4,
			StartPosition:  Point{Row: 0, Column: 1},
			OldEndPosition: Point{Row: 0, Column: 11},
			NewEndPosition: Point{Row: 0, Column: 4},
		})

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)
		child3 := expr.Child(2)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 4)
		assert.EqualValues(t, expr.EndByte(), 8)
		assert.True(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 4)
		assert.EqualValues(t, child1.EndByte(), 4)
		assert.True(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 4)
		assert.EqualValues(t, child2.EndByte(), 4)
		assert.True(t, child3.HasChanges())
		assert.EqualValues(t, child3.StartByte(), 5)
		assert.EqualValues(t, child3.EndByte(), 8)
	}

	// insertion at the end of the tree:
	// extend the tree's content.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      15,
			OldEndByte:     15,
			NewEndByte:     16,
			StartPosition:  Point{Row: 0, Column: 15},
			OldEndPosition: Point{Row: 0, Column: 15},
			NewEndPosition: Point{Row: 0, Column: 16},
		})

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)
		child3 := expr.Child(2)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 2)
		assert.EqualValues(t, expr.EndByte(), 16)
		assert.False(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 2)
		assert.EqualValues(t, child1.EndByte(), 5)
		assert.False(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 7)
		assert.EqualValues(t, child2.EndByte(), 10)
		assert.True(t, child3.HasChanges())
		assert.EqualValues(t, child3.StartByte(), 12)
		assert.EqualValues(t, child3.EndByte(), 16)
	}

	// replacement that starts within a token and extends beyond the end of the tree:
	// resize the token and empty out any subsequent child nodes.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      3,
			OldEndByte:     90,
			NewEndByte:     4,
			StartPosition:  Point{Row: 0, Column: 3},
			OldEndPosition: Point{Row: 0, Column: 90},
			NewEndPosition: Point{Row: 0, Column: 4},
		})

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)
		child3 := expr.Child(2)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 2)
		assert.EqualValues(t, expr.EndByte(), 4)
		assert.True(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 2)
		assert.EqualValues(t, child1.EndByte(), 4)
		assert.True(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 4)
		assert.EqualValues(t, child2.EndByte(), 4)
		assert.True(t, child3.HasChanges())
		assert.EqualValues(t, child3.StartByte(), 4)
		assert.EqualValues(t, child3.EndByte(), 4)
	}

	// replacement that starts in whitespace and extends beyond the end of the tree:
	// shift the token's start position and empty out its content.
	{
		tree := tree.Clone()
		tree.Edit(&InputEdit{
			StartByte:      6,
			OldEndByte:     90,
			NewEndByte:     8,
			StartPosition:  Point{Row: 0, Column: 6},
			OldEndPosition: Point{Row: 0, Column: 90},
			NewEndPosition: Point{Row: 0, Column: 8},
		})

		expr := tree.RootNode().Child(0).Child(0)
		child1 := expr.Child(0)
		child2 := expr.Child(1)
		child3 := expr.Child(2)

		assert.True(t, expr.HasChanges())
		assert.EqualValues(t, expr.StartByte(), 2)
		assert.EqualValues(t, expr.EndByte(), 8)
		assert.False(t, child1.HasChanges())
		assert.EqualValues(t, child1.StartByte(), 2)
		assert.EqualValues(t, child1.EndByte(), 5)
		assert.True(t, child2.HasChanges())
		assert.EqualValues(t, child2.StartByte(), 8)
		assert.EqualValues(t, child2.EndByte(), 8)
		assert.True(t, child3.HasChanges())
		assert.EqualValues(t, child3.StartByte(), 8)
		assert.EqualValues(t, child3.EndByte(), 8)
	}
}

func TestTreeEditWithIncludedRanges(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("html"))

	source := "<div><% if a %><span>a</span><% else %><span>b</span><% end %></div>"

	ranges := []Range{
		{StartByte: 0, EndByte: 5, StartPoint: Point{Row: 0, Column: 0}, EndPoint: Point{Row: 0, Column: 5}},
		{StartByte: 15, EndByte: 29, StartPoint: Point{Row: 0, Column: 15}, EndPoint: Point{Row: 0, Column: 29}},
		{StartByte: 39, EndByte: 53, StartPoint: Point{Row: 0, Column: 39}, EndPoint: Point{Row: 0, Column: 53}},
		{StartByte: 62, EndByte: 68, StartPoint: Point{Row: 0, Column: 62}, EndPoint: Point{Row: 0, Column: 68}},
	}

	parser.SetIncludedRanges(ranges)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	tree.Edit(&InputEdit{
		StartByte:      29,
		OldEndByte:     53,
		NewEndByte:     29,
		StartPosition:  Point{Row: 0, Column: 29},
		OldEndPosition: Point{Row: 0, Column: 53},
		NewEndPosition: Point{Row: 0, Column: 29},
	})

	assert.Equal(t,
		[]Range{
			{StartByte: 0, EndByte: 5, StartPoint: Point{Row: 0, Column: 0}, EndPoint: Point{Row: 0, Column: 5}},
			{StartByte: 15, EndByte: 29, StartPoint: Point{Row: 0, Column: 15}, EndPoint: Point{Row: 0, Column: 29}},
			{StartByte: 29, EndByte: 29, StartPoint: Point{Row: 0, Column: 29}, EndPoint: Point{Row: 0, Column: 29}},
			{StartByte: 38, EndByte: 44, StartPoint: Point{Row: 0, Column: 38}, EndPoint: Point{Row: 0, Column: 44}},
		},
		tree.IncludedRanges(),
	)
}

func TestTreeCursor(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	tree := parser.Parse([]byte(`
                struct Stuff {
                    a: A,
                    b: Option<B>,
                }
	`), nil)

	cursor := tree.Walk()
	assert.Equal(t, cursor.Node().Kind(), "source_file")

	assert.True(t, cursor.GotoFirstChild())
	assert.Equal(t, cursor.Node().Kind(), "struct_item")

	assert.True(t, cursor.GotoFirstChild())
	assert.Equal(t, cursor.Node().Kind(), "struct")
	assert.False(t, cursor.Node().IsNamed())

	assert.True(t, cursor.GotoNextSibling())
	assert.Equal(t, cursor.Node().Kind(), "type_identifier")
	assert.True(t, cursor.Node().IsNamed())

	assert.True(t, cursor.GotoNextSibling())
	assert.Equal(t, cursor.Node().Kind(), "field_declaration_list")
	assert.True(t, cursor.Node().IsNamed())

	assert.True(t, cursor.GotoLastChild())
	assert.Equal(t, cursor.Node().Kind(), "}")
	assert.False(t, cursor.Node().IsNamed())
	assert.Equal(t, cursor.Node().StartPosition(), Point{Row: 4, Column: 16})

	assert.True(t, cursor.GotoPreviousSibling())
	assert.Equal(t, cursor.Node().Kind(), ",")
	assert.False(t, cursor.Node().IsNamed())
	assert.Equal(t, cursor.Node().StartPosition(), Point{Row: 3, Column: 32})

	assert.True(t, cursor.GotoPreviousSibling())
	assert.Equal(t, cursor.Node().Kind(), "field_declaration")
	assert.True(t, cursor.Node().IsNamed())
	assert.Equal(t, cursor.Node().StartPosition(), Point{Row: 3, Column: 20})

	assert.True(t, cursor.GotoPreviousSibling())
	assert.Equal(t, cursor.Node().Kind(), ",")
	assert.False(t, cursor.Node().IsNamed())
	assert.Equal(t, cursor.Node().StartPosition(), Point{Row: 2, Column: 24})

	assert.True(t, cursor.GotoPreviousSibling())
	assert.Equal(t, cursor.Node().Kind(), "field_declaration")
	assert.True(t, cursor.Node().IsNamed())
	assert.Equal(t, cursor.Node().StartPosition(), Point{Row: 2, Column: 20})

	assert.True(t, cursor.GotoPreviousSibling())
	assert.Equal(t, cursor.Node().Kind(), "{")
	assert.False(t, cursor.Node().IsNamed())
	assert.Equal(t, cursor.Node().StartPosition(), Point{Row: 1, Column: 29})

	copy := tree.Walk()
	copy.ResetTo(cursor)

	assert.Equal(t, copy.Node().Kind(), "{")
	assert.False(t, copy.Node().IsNamed())

	assert.True(t, copy.GotoParent())
	assert.Equal(t, copy.Node().Kind(), "field_declaration_list")
	assert.True(t, copy.Node().IsNamed())

	assert.True(t, copy.GotoParent())
	assert.Equal(t, copy.Node().Kind(), "struct_item")
}

func TestTreeCursorPreviousSibling(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	text := `
    // Hi there
    // This is fun!
    // Another one!
`
	tree := parser.Parse([]byte(text), nil)
	defer tree.Close()

	cursor := tree.Walk()
	assert.Equal(t, cursor.Node().Kind(), "source_file")

	assert.True(t, cursor.GotoLastChild())
	assert.Equal(t, cursor.Node().Kind(), "line_comment")
	assert.Equal(t, cursor.Node().Utf8Text([]byte(text)), "// Another one!")

	assert.True(t, cursor.GotoPreviousSibling())
	assert.Equal(t, cursor.Node().Kind(), "line_comment")
	assert.Equal(t, cursor.Node().Utf8Text([]byte(text)), "// This is fun!")

	assert.True(t, cursor.GotoPreviousSibling())
	assert.Equal(t, cursor.Node().Kind(), "line_comment")
	assert.Equal(t, cursor.Node().Utf8Text([]byte(text)), "// Hi there")
}

func TestTreeCursorFields(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	tree := parser.Parse([]byte("function /*1*/ bar /*2*/ () {}"), nil)
	defer tree.Close()

	cursor := tree.Walk()
	assert.Equal(t, cursor.Node().Kind(), "program")

	cursor.GotoFirstChild()
	assert.Equal(t, cursor.Node().Kind(), "function_declaration")
	assert.Equal(t, cursor.FieldName(), "")

	cursor.GotoFirstChild()
	assert.Equal(t, cursor.Node().Kind(), "function")
	assert.Equal(t, cursor.FieldName(), "")

	cursor.GotoNextSibling()
	assert.Equal(t, cursor.Node().Kind(), "comment")
	assert.Equal(t, cursor.FieldName(), "")

	cursor.GotoNextSibling()
	assert.Equal(t, cursor.Node().Kind(), "identifier")
	assert.Equal(t, cursor.FieldName(), "name")

	cursor.GotoNextSibling()
	assert.Equal(t, cursor.Node().Kind(), "comment")
	assert.Equal(t, cursor.FieldName(), "")

	cursor.GotoNextSibling()
	assert.Equal(t, cursor.Node().Kind(), "formal_parameters")
	assert.Equal(t, cursor.FieldName(), "parameters")
}

func TestTreeCursorChildForPoint(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	source := `
    [
        one,
        {
            two: tree
        },
        four, five, six
    ];`[1:]
	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	c := tree.Walk()
	assert.Equal(t, c.Node().Kind(), "program")

	assert.Nil(t, c.GotoFirstChildForPoint(Point{Row: 7, Column: 0}))
	assert.Nil(t, c.GotoFirstChildForPoint(Point{Row: 6, Column: 7}))
	assert.Equal(t, c.Node().Kind(), "program")

	// descend to expression statement
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 6, Column: 5}), 0)
	assert.Equal(t, c.Node().Kind(), "expression_statement")

	// step into ';' and back up
	assert.Nil(t, c.GotoFirstChildForPoint(Point{Row: 7, Column: 0}))
	assert.Nil(t, c.GotoFirstChildForPoint(Point{Row: 6, Column: 6}))
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 6, Column: 5}), 1)
	assert.Equal(t, c.Node().Kind(), ";")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 6, Column: 5})
	assert.True(t, c.GotoParent())

	// descend into array
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 6, Column: 4}), 0)
	assert.Equal(t, c.Node().Kind(), "array")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 0, Column: 4})

	// step into '[' and back up
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 0, Column: 4}), 0)
	assert.Equal(t, c.Node().Kind(), "[")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 0, Column: 4})
	assert.True(t, c.GotoParent())

	// step into identifier 'one' and back up
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 1, Column: 0}), 1)
	assert.Equal(t, c.Node().Kind(), "identifier")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 1, Column: 8})
	assert.True(t, c.GotoParent())
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 1, Column: 10}), 1)
	assert.Equal(t, c.Node().Kind(), "identifier")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 1, Column: 8})
	assert.True(t, c.GotoParent())

	// step into first ',' and back up
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 1, Column: 11}), 2)
	assert.Equal(t, c.Node().Kind(), ",")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 1, Column: 11})
	assert.True(t, c.GotoParent())

	// step into identifier 'four' and back up
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 5, Column: 0}), 5)
	assert.Equal(t, c.Node().Kind(), "identifier")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 5, Column: 8})
	assert.True(t, c.GotoParent())
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 5, Column: 0}), 5)
	assert.Equal(t, c.Node().Kind(), "identifier")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 5, Column: 8})
	assert.True(t, c.GotoParent())

	// step into ']' and back up
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 6, Column: 0}), 10)
	assert.Equal(t, c.Node().Kind(), "]")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 6, Column: 4})
	assert.True(t, c.GotoParent())
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 6, Column: 0}), 10)
	assert.Equal(t, c.Node().Kind(), "]")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 6, Column: 4})
	assert.True(t, c.GotoParent())

	// descend into object
	assert.EqualValues(t, *c.GotoFirstChildForPoint(Point{Row: 2, Column: 0}), 3)
	assert.Equal(t, c.Node().Kind(), "object")
	assert.Equal(t, c.Node().StartPosition(), Point{Row: 2, Column: 8})
}

func TestTreeNodeEquality(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))
	tree := parser.Parse([]byte("struct A {}"), nil)
	defer tree.Close()
	node1 := tree.RootNode()
	node2 := tree.RootNode()

	assert.Equal(t, node1, node2)
	assert.Equal(t, node1.Child(0), node2.Child(0))
	assert.NotEqual(t, node1.Child(0), node2)
}

func TestGetChangedRanges(t *testing.T) {
	sourceCode := []byte("{a: null};\n")

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse(sourceCode, nil)
	defer tree.Close()

	assert.Equal(t, tree.RootNode().ToSexp(), "(program (expression_statement (object (pair key: (property_identifier) value: (null)))))")

	// Updating one token
	{
		tree := tree.Clone()
		sourceCode := append([]byte(nil), sourceCode...)

		// Replace `null` with `nothing` - that token has changed syntax
		edit := &testEdit{
			position:      indexOf(sourceCode, "ull"),
			deletedLength: 3,
			insertedText:  []byte("othing"),
		}
		inverseEdit := invertEdit(sourceCode, edit)
		ranges := getChangedRanges(parser, tree, &sourceCode, edit)
		assert.Equal(t, []Range{rangeOf(sourceCode, "nothing")}, ranges)

		// Replace `nothing` with `null` - that token has changed syntax
		ranges = getChangedRanges(parser, tree, &sourceCode, inverseEdit)
		assert.Equal(t, []Range{rangeOf(sourceCode, "null")}, ranges)
	}

	// Changing only leading whitespace
	{
		tree := tree.Clone()
		sourceCode := append([]byte(nil), sourceCode...)

		// Insert leading newline - no changed ranges
		edit := &testEdit{
			position:      0,
			deletedLength: 0,
			insertedText:  []byte("\n"),
		}
		inverseEdit := invertEdit(sourceCode, edit)
		ranges := getChangedRanges(parser, tree, &sourceCode, edit)
		assert.Equal(t, []Range{}, ranges)

		// Remove leading newline - no changed ranges
		ranges = getChangedRanges(parser, tree, &sourceCode, inverseEdit)
		assert.Equal(t, []Range{}, ranges)
	}

	// Inserting elements
	{
		tree := tree.Clone()
		sourceCode := append([]byte(nil), sourceCode...)

		// Insert a key-value pair before the `}` - those tokens are changed
		edit1 := &testEdit{
			position:      indexOf(sourceCode, "}"),
			deletedLength: 0,
			insertedText:  []byte(", b: false"),
		}
		inverseEdit1 := invertEdit(sourceCode, edit1)
		ranges := getChangedRanges(parser, tree, &sourceCode, edit1)
		assert.Equal(t, []Range{rangeOf(sourceCode, ", b: false")}, ranges)

		edit2 := &testEdit{
			position:      indexOf(sourceCode, ", b"),
			deletedLength: 0,
			insertedText:  []byte(", c: 1"),
		}
		inverseEdit2 := invertEdit(sourceCode, edit2)
		ranges = getChangedRanges(parser, tree, &sourceCode, edit2)
		assert.Equal(t, []Range{rangeOf(sourceCode, ", c: 1")}, ranges)

		// Remove the middle pair
		ranges = getChangedRanges(parser, tree, &sourceCode, inverseEdit2)
		assert.Equal(t, []Range{}, ranges)

		// Remove the second pair
		ranges = getChangedRanges(parser, tree, &sourceCode, inverseEdit1)
		assert.Equal(t, []Range{}, ranges)
	}

	// Wrapping elements in larger expressions
	{
		sourceCode := append([]byte(nil), sourceCode...)

		// Replace `null` with the binary expression `b === null`
		edit1 := &testEdit{
			position:      indexOf(sourceCode, "null"),
			deletedLength: 0,
			insertedText:  []byte("b === "),
		}
		inverseEdit1 := invertEdit(sourceCode, edit1)
		ranges := getChangedRanges(parser, tree, &sourceCode, edit1)
		assert.Equal(t, []Range{rangeOf(sourceCode, "b === null")}, ranges)

		// Undo
		ranges = getChangedRanges(parser, tree, &sourceCode, inverseEdit1)
		assert.Equal(t, []Range{rangeOf(sourceCode, "null")}, ranges)
	}
}

func TestConsistencyWithMidCodepointEdit(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("php"))
	sourceCode := []byte("\n<?php\n\n<<<'\xE5\xAD\x97\xE6\xBC\xA2'\n  T\n\xE5\xAD\x97\xE6\xBC\xA2;")
	tree := parser.Parse(sourceCode, nil)
	defer tree.Close()

	edit := &testEdit{
		position:      17,
		deletedLength: 0,
		insertedText:  []byte{46},
	}
	performEdit(tree, &sourceCode, edit)
	tree2 := parser.Parse(sourceCode, tree)

	inverseEdit := invertEdit(sourceCode, edit)
	performEdit(tree2, &sourceCode, inverseEdit)
	tree3 := parser.Parse(sourceCode, tree2)

	assert.Equal(t, tree3.RootNode().ToSexp(), tree.RootNode().ToSexp())
}

func indexOf(text []byte, substring string) uint {
	return uint(strings.Index(string(text), substring))
}

func rangeOf(text []byte, substring string) Range {
	startByte := indexOf(text, substring)
	endByte := startByte + uint(len(substring))
	return Range{
		StartByte:  startByte,
		EndByte:    endByte,
		StartPoint: Point{Row: 0, Column: startByte},
		EndPoint:   Point{Row: 0, Column: endByte},
	}
}

func getChangedRanges(parser *Parser, tree *Tree, sourceCode *[]byte, edit *testEdit) []Range {
	performEdit(tree, sourceCode, edit)
	newTree := parser.Parse(*sourceCode, tree)
	result := tree.ChangedRanges(newTree)
	tree.Close()
	*tree = *newTree
	return result
}
