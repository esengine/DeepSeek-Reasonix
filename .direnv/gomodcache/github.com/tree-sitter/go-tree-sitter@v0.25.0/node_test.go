package tree_sitter_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	. "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

const JSON_EXAMPLE = `

[
  123,
  false,
  {
    "x": null
  }
]
`

func ExampleNode() {
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

	rootNode := tree.RootNode()
	fmt.Println(rootNode.Kind())
	fmt.Println(rootNode.StartPosition())
	fmt.Println(rootNode.EndPosition())

	functionNode := rootNode.Child(1)
	fmt.Println(functionNode.Kind())
	fmt.Println(functionNode.ChildByFieldName("name").Kind())

	functionNameNode := functionNode.Child(1)
	fmt.Println(functionNameNode.StartPosition())
	fmt.Println(functionNameNode.EndPosition())

	// Output:
	// source_file
	// {1 3}
	// {7 2}
	// function_declaration
	// identifier
	// {4 8}
	// {4 12}
}

func TestNodeChild(t *testing.T) {
	tree := parseJsonExample()
	arrayNode := tree.RootNode().Child(0)

	assert.Equal(t, "array", arrayNode.Kind())
	assert.EqualValues(t, 3, arrayNode.NamedChildCount())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "["), arrayNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "]")+1, arrayNode.EndByte())
	assert.Equal(t, Point{2, 0}, arrayNode.StartPosition())
	assert.Equal(t, Point{8, 1}, arrayNode.EndPosition())
	assert.EqualValues(t, 7, arrayNode.ChildCount())

	leftBracketNode := arrayNode.Child(0)
	numberNode := arrayNode.Child(1)
	commaNode1 := arrayNode.Child(2)
	falseNode := arrayNode.Child(3)
	commaNode2 := arrayNode.Child(4)
	objectNode := arrayNode.Child(5)
	rightBracketNode := arrayNode.Child(6)

	assert.Equal(t, "[", leftBracketNode.Kind())
	assert.Equal(t, "number", numberNode.Kind())
	assert.Equal(t, ",", commaNode1.Kind())
	assert.Equal(t, "false", falseNode.Kind())
	assert.Equal(t, ",", commaNode2.Kind())
	assert.Equal(t, "object", objectNode.Kind())
	assert.Equal(t, "]", rightBracketNode.Kind())

	assert.False(t, leftBracketNode.IsNamed())
	assert.True(t, numberNode.IsNamed())
	assert.False(t, commaNode1.IsNamed())
	assert.True(t, falseNode.IsNamed())
	assert.False(t, commaNode2.IsNamed())
	assert.True(t, objectNode.IsNamed())
	assert.False(t, rightBracketNode.IsNamed())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "123"), numberNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "123")+3, numberNode.EndByte())
	assert.Equal(t, Point{3, 2}, numberNode.StartPosition())
	assert.Equal(t, Point{3, 5}, numberNode.EndPosition())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "false"), falseNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "false")+5, falseNode.EndByte())
	assert.Equal(t, Point{4, 2}, falseNode.StartPosition())
	assert.Equal(t, Point{4, 7}, falseNode.EndPosition())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "{"), objectNode.StartByte())
	assert.Equal(t, Point{5, 2}, objectNode.StartPosition())
	assert.Equal(t, Point{7, 3}, objectNode.EndPosition())
	assert.EqualValues(t, 3, objectNode.ChildCount())

	leftBraceNode := objectNode.Child(0)
	pairNode := objectNode.Child(1)
	rightBraceNode := objectNode.Child(2)

	assert.Equal(t, "{", leftBraceNode.Kind())
	assert.Equal(t, "pair", pairNode.Kind())
	assert.Equal(t, "}", rightBraceNode.Kind())

	assert.False(t, leftBraceNode.IsNamed())
	assert.True(t, pairNode.IsNamed())
	assert.False(t, rightBraceNode.IsNamed())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "\"x\""), pairNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "null")+4, pairNode.EndByte())
	assert.Equal(t, Point{6, 4}, pairNode.StartPosition())
	assert.Equal(t, Point{6, 13}, pairNode.EndPosition())
	assert.EqualValues(t, 3, pairNode.ChildCount())

	stringNode := pairNode.Child(0)
	colonNode := pairNode.Child(1)
	nullNode := pairNode.Child(2)

	assert.Equal(t, "string", stringNode.Kind())
	assert.Equal(t, ":", colonNode.Kind())
	assert.Equal(t, "null", nullNode.Kind())

	assert.True(t, stringNode.IsNamed())
	assert.False(t, colonNode.IsNamed())
	assert.True(t, nullNode.IsNamed())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "\"x\""), stringNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "\"x\"")+3, stringNode.EndByte())
	assert.Equal(t, Point{6, 4}, stringNode.StartPosition())
	assert.Equal(t, Point{6, 7}, stringNode.EndPosition())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "null"), nullNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "null")+4, nullNode.EndByte())
	assert.Equal(t, Point{6, 9}, nullNode.StartPosition())
	assert.Equal(t, Point{6, 13}, nullNode.EndPosition())

	assert.Equal(t, pairNode, stringNode.Parent())
	assert.Equal(t, pairNode, nullNode.Parent())
	assert.Equal(t, objectNode, pairNode.Parent())
	assert.Equal(t, arrayNode, numberNode.Parent())
	assert.Equal(t, arrayNode, falseNode.Parent())
	assert.Equal(t, arrayNode, objectNode.Parent())
	assert.Equal(t, tree.RootNode(), arrayNode.Parent())
	assert.Nil(t, tree.RootNode().Parent())

	assert.Equal(t, arrayNode, tree.RootNode().ChildWithDescendant(nullNode))
	assert.Equal(t, objectNode, arrayNode.ChildWithDescendant(nullNode))
	assert.Equal(t, pairNode, objectNode.ChildWithDescendant(nullNode))
	assert.Equal(t, nullNode, pairNode.ChildWithDescendant(nullNode))
	assert.Nil(t, nullNode.ChildWithDescendant(nullNode))
}

func TestNodeChildren(t *testing.T) {
	tree := parseJsonExample()
	cursor := tree.Walk()
	arrayNode := tree.RootNode().Child(0)

	children := arrayNode.Children(cursor)
	var kinds []string
	for _, child := range children {
		kinds = append(kinds, child.Kind())
	}
	assert.Equal(t, []string{"[", "number", ",", "false", ",", "object", "]"}, kinds)

	namedChildren := arrayNode.NamedChildren(cursor)
	var namedKinds []string
	for _, child := range namedChildren {
		namedKinds = append(namedKinds, child.Kind())
	}
	assert.Equal(t, []string{"number", "false", "object"}, namedKinds)

	namedChildren = arrayNode.NamedChildren(cursor)
	var objectNode *Node
	for _, child := range namedChildren {
		if child.Kind() == "object" {
			objectNode = &child
			break
		}
	}
	assert.NotNil(t, objectNode)

	children = objectNode.Children(cursor)
	var objectKinds []string
	for _, child := range children {
		objectKinds = append(objectKinds, child.Kind())
	}
	assert.Equal(t, []string{"{", "pair", "}"}, objectKinds)
}

func TestNodeChildrenByFieldName(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("python"))
	source := `
        if one:
            a()
        elif two:
            b()
        elif three:
            c()
        elif four:
            d()
	`

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()
	node := tree.RootNode().Child(0)
	assert.Equal(t, "if_statement", node.Kind())

	cursor := tree.Walk()
	alternatives := node.ChildrenByFieldName("alternative", cursor)
	var alternativeTexts []string
	for _, alternative := range alternatives {
		condition := alternative.ChildByFieldName("condition")
		alternativeTexts = append(alternativeTexts, string(source[condition.StartByte():condition.EndByte()]))
	}
	assert.Equal(t, []string{"two", "three", "four"}, alternativeTexts)
}

func TestNodeParentOfChildByFieldName(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	tree := parser.Parse([]byte("foo(a().b[0].c.d.e())"), nil)
	defer tree.Close()
	callNode := tree.RootNode().NamedChild(0).NamedChild(0)
	assert.Equal(t, "call_expression", callNode.Kind())

	// Regression test - when a field points to a hidden node (in this case, `_expression`)
	// the hidden node should not be added to the node parent cache.
	assert.Equal(t, callNode, callNode.ChildByFieldName("function").Parent())
}

func TestParentOfZeroWithNode(t *testing.T) {
	code := "def dupa(foo):"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("python"))

	tree := parser.Parse([]byte(code), nil)
	defer tree.Close()
	root := tree.RootNode()
	functionDefinition := root.Child(0)
	block := functionDefinition.Child(4)
	blockParent := block.Parent()

	assert.Equal(t, "(block)", block.ToSexp())
	assert.Equal(t, "function_definition", blockParent.Kind())
	assert.Equal(t, "(function_definition name: (identifier) parameters: (parameters (identifier)) body: (block))", blockParent.ToSexp())

	assert.Equal(t, functionDefinition, root.ChildWithDescendant(block))
	assert.Equal(t, block, functionDefinition.ChildWithDescendant(block))
	assert.Nil(t, block.ChildWithDescendant(block))
}

func TestFirstChildForOffset(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse([]byte("x10 + 100"), nil)
	defer tree.Close()

	sumNode := tree.RootNode().Child(0).Child(0)

	assert.Equal(t, "identifier", sumNode.FirstChildForByte(0).Kind())
	assert.Equal(t, "identifier", sumNode.FirstChildForByte(1).Kind())
	assert.Equal(t, "+", sumNode.FirstChildForByte(3).Kind())
	assert.Equal(t, "number", sumNode.FirstChildForByte(5).Kind())
}

func TestFirstNamedChildForOffset(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse([]byte("x10 + 100"), nil)
	defer tree.Close()

	sumNode := tree.RootNode().Child(0).Child(0)

	assert.Equal(t, "identifier", sumNode.FirstNamedChildForByte(0).Kind())
	assert.Equal(t, "identifier", sumNode.FirstNamedChildForByte(1).Kind())
	assert.Equal(t, "number", sumNode.FirstNamedChildForByte(3).Kind())
}

func TestNodeFieldNameForChild(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("c"))

	tree := parser.Parse([]byte("int w = x + /* y is special! */ y;"), nil)
	defer tree.Close()
	translationUnitNode := tree.RootNode()
	declarationNode := translationUnitNode.NamedChild(0)

	binaryExpressionNode := declarationNode.ChildByFieldName("declarator").ChildByFieldName("value")

	// -------------------
	// left: (identifier)  0
	// operator: "+"       1 <--- (not a named child)
	// (comment)           2 <--- (is an extra)
	// right: (identifier) 3
	// -------------------

	assert.Equal(t, "left", binaryExpressionNode.FieldNameForChild(0))
	assert.Equal(t, "operator", binaryExpressionNode.FieldNameForChild(1))
	// The comment should not have a field name, as it's just an extra
	assert.Equal(t, "", binaryExpressionNode.FieldNameForChild(2))
	assert.Equal(t, "right", binaryExpressionNode.FieldNameForChild(3))
	// Negative test - Not a valid child index
	assert.Equal(t, "", binaryExpressionNode.FieldNameForChild(4))
}

func TestNodeFieldNameForNamedChild(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("c"))

	tree := parser.Parse([]byte("int w = x + /* y is special! */ y;"), nil)
	defer tree.Close()
	translationUnitNode := tree.RootNode()
	declarationNode := translationUnitNode.NamedChild(0)

	binaryExpressionNode := declarationNode.ChildByFieldName("declarator").ChildByFieldName("value")

	// -------------------
	// left: (identifier)  0
	// operator: "+"       _ <--- (not a named child)
	// (comment)           1 <--- (is an extra)
	// right: (identifier) 2
	// -------------------

	assert.Equal(t, "left", binaryExpressionNode.FieldNameForNamedChild(0))
	// The comment should not have a field name, as it's just an extra
	assert.Equal(t, "", binaryExpressionNode.FieldNameForNamedChild(1))
	// The operator is not a named child, so the named child at index 2 is the right child
	assert.Equal(t, "right", binaryExpressionNode.FieldNameForNamedChild(2))
	// Negative test - Not a valid child index
	assert.Equal(t, "", binaryExpressionNode.FieldNameForNamedChild(3))
}

func TestNodeChildByFieldNameWithExtraHiddenChildren(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("python"))

	// In the Python grammar, some fields are applied to `suite` nodes,
	// which consist of an invisible `indent` token followed by a block.
	// Check that when searching for a child with a field name, we don't
	tree := parser.Parse([]byte("while a:\n  pass"), nil)
	defer tree.Close()
	whileNode := tree.RootNode().Child(0)
	assert.Equal(t, "while_statement", whileNode.Kind())
	assert.Equal(t, whileNode.Child(3), whileNode.ChildByFieldName("body"))
}

func TestNodeNamedChild(t *testing.T) {
	tree := parseJsonExample()
	arrayNode := tree.RootNode().Child(0)

	numberNode := arrayNode.NamedChild(0)
	falseNode := arrayNode.NamedChild(1)
	objectNode := arrayNode.NamedChild(2)

	assert.Equal(t, "number", numberNode.Kind())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "123"), numberNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "123")+3, numberNode.EndByte())
	assert.Equal(t, Point{3, 2}, numberNode.StartPosition())
	assert.Equal(t, Point{3, 5}, numberNode.EndPosition())

	assert.Equal(t, "false", falseNode.Kind())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "false"), falseNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "false")+5, falseNode.EndByte())
	assert.Equal(t, Point{4, 2}, falseNode.StartPosition())
	assert.Equal(t, Point{4, 7}, falseNode.EndPosition())

	assert.Equal(t, "object", objectNode.Kind())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "{"), objectNode.StartByte())
	assert.Equal(t, Point{5, 2}, objectNode.StartPosition())
	assert.Equal(t, Point{7, 3}, objectNode.EndPosition())
	assert.EqualValues(t, 1, objectNode.NamedChildCount())

	pairNode := objectNode.NamedChild(0)
	assert.Equal(t, "pair", pairNode.Kind())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "\"x\""), pairNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "null")+4, pairNode.EndByte())
	assert.Equal(t, Point{6, 4}, pairNode.StartPosition())
	assert.Equal(t, Point{6, 13}, pairNode.EndPosition())

	stringNode := pairNode.NamedChild(0)
	nullNode := pairNode.NamedChild(1)

	assert.Equal(t, "string", stringNode.Kind())
	assert.Equal(t, "null", nullNode.Kind())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "\"x\""), stringNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "\"x\"")+3, stringNode.EndByte())
	assert.Equal(t, Point{6, 4}, stringNode.StartPosition())
	assert.Equal(t, Point{6, 7}, stringNode.EndPosition())

	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "null"), nullNode.StartByte())
	assert.EqualValues(t, strings.Index(JSON_EXAMPLE, "null")+4, nullNode.EndByte())
	assert.Equal(t, Point{6, 9}, nullNode.StartPosition())
	assert.Equal(t, Point{6, 13}, nullNode.EndPosition())

	assert.Equal(t, pairNode, stringNode.Parent())
	assert.Equal(t, pairNode, nullNode.Parent())
	assert.Equal(t, objectNode, pairNode.Parent())
	assert.Equal(t, arrayNode, numberNode.Parent())
	assert.Equal(t, arrayNode, falseNode.Parent())
	assert.Equal(t, arrayNode, objectNode.Parent())
	assert.Equal(t, tree.RootNode(), arrayNode.Parent())
	assert.Nil(t, tree.RootNode().Parent())

	assert.Equal(t, arrayNode, tree.RootNode().ChildWithDescendant(nullNode))
	assert.Equal(t, objectNode, arrayNode.ChildWithDescendant(nullNode))
	assert.Equal(t, pairNode, objectNode.ChildWithDescendant(nullNode))
	assert.Equal(t, nullNode, pairNode.ChildWithDescendant(nullNode))
	assert.Nil(t, nullNode.ChildWithDescendant(nullNode))
}

func TestNodeDescendantCount(t *testing.T) {
	tree := parseJsonExample()
	valueNode := tree.RootNode()
	allNodes := getAllNodes(tree)

	assert.EqualValues(t, len(allNodes), valueNode.DescendantCount())

	cursor := valueNode.Walk()
	for i, node := range allNodes {
		cursor.GotoDescendant(uint32(i))
		assert.Equal(t, node, cursor.Node())
	}

	for i := len(allNodes) - 1; i >= 0; i-- {
		cursor.GotoDescendant(uint32(i))
		assert.Equal(t, allNodes[i], cursor.Node())
	}
}

func TestDescendantCountSingleNodeTree(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("embedded-template"))
	tree := parser.Parse([]byte("hello"), nil)
	defer tree.Close()

	allNodes := getAllNodes(tree)
	assert.EqualValues(t, 2, len(allNodes))
	assert.EqualValues(t, 2, tree.RootNode().DescendantCount())

	cursor := tree.RootNode().Walk()

	cursor.GotoDescendant(0)
	assert.EqualValues(t, 0, cursor.Depth())
	assert.Equal(t, allNodes[0], cursor.Node())
	cursor.GotoDescendant(1)
	assert.EqualValues(t, 1, cursor.Depth())
	assert.Equal(t, allNodes[1], cursor.Node())
}

func TestNodeDescendantForRange(t *testing.T) {
	tree := parseJsonExample()
	arrayNode := tree.RootNode()

	// Leaf node exactly matches the given bounds - byte query
	colonIndex := strings.Index(JSON_EXAMPLE, ":")
	colonNode := arrayNode.DescendantForByteRange(uint(colonIndex), uint(colonIndex+1))
	assert.NotNil(t, colonNode)
	assert.Equal(t, ":", colonNode.Kind())
	assert.EqualValues(t, colonIndex, colonNode.StartByte())
	assert.EqualValues(t, colonIndex+1, colonNode.EndByte())
	assert.Equal(t, Point{6, 7}, colonNode.StartPosition())
	assert.Equal(t, Point{6, 8}, colonNode.EndPosition())

	// Leaf node exactly matches the given bounds - point query
	colonNode = arrayNode.DescendantForPointRange(Point{6, 7}, Point{6, 8})
	assert.NotNil(t, colonNode)
	assert.Equal(t, ":", colonNode.Kind())
	assert.EqualValues(t, colonIndex, colonNode.StartByte())
	assert.EqualValues(t, colonIndex+1, colonNode.EndByte())
	assert.Equal(t, Point{6, 7}, colonNode.StartPosition())
	assert.Equal(t, Point{6, 8}, colonNode.EndPosition())

	// The given point is between two adjacent leaf nodes - byte query
	colonNode = arrayNode.DescendantForByteRange(uint(colonIndex), uint(colonIndex))
	assert.NotNil(t, colonNode)
	assert.Equal(t, ":", colonNode.Kind())
	assert.EqualValues(t, colonIndex, colonNode.StartByte())
	assert.EqualValues(t, colonIndex+1, colonNode.EndByte())
	assert.Equal(t, Point{6, 7}, colonNode.StartPosition())
	assert.Equal(t, Point{6, 8}, colonNode.EndPosition())

	// The given point is between two adjacent leaf nodes - point query
	colonNode = arrayNode.DescendantForPointRange(Point{6, 7}, Point{6, 7})
	assert.NotNil(t, colonNode)
	assert.Equal(t, ":", colonNode.Kind())
	assert.EqualValues(t, colonIndex, colonNode.StartByte())
	assert.EqualValues(t, colonIndex+1, colonNode.EndByte())
	assert.Equal(t, Point{6, 7}, colonNode.StartPosition())
	assert.Equal(t, Point{6, 8}, colonNode.EndPosition())

	// Leaf node starts at the lower bound, ends after the upper bound - byte query
	stringIndex := strings.Index(JSON_EXAMPLE, "\"x\"")
	stringNode := arrayNode.DescendantForByteRange(uint(stringIndex), uint(stringIndex+2))
	assert.NotNil(t, stringNode)
	assert.Equal(t, "string", stringNode.Kind())
	assert.EqualValues(t, stringIndex, stringNode.StartByte())
	assert.EqualValues(t, stringIndex+3, stringNode.EndByte())
	assert.Equal(t, Point{6, 4}, stringNode.StartPosition())
	assert.Equal(t, Point{6, 7}, stringNode.EndPosition())

	// Leaf node starts at the lower bound, ends after the upper bound - point query
	stringNode = arrayNode.DescendantForPointRange(Point{6, 4}, Point{6, 6})
	assert.NotNil(t, stringNode)
	assert.Equal(t, "string", stringNode.Kind())
	assert.EqualValues(t, stringIndex, stringNode.StartByte())
	assert.EqualValues(t, stringIndex+3, stringNode.EndByte())
	assert.Equal(t, Point{6, 4}, stringNode.StartPosition())
	assert.Equal(t, Point{6, 7}, stringNode.EndPosition())

	// Leaf node starts before the lower bound, ends at the upper bound - byte query
	nullIndex := strings.Index(JSON_EXAMPLE, "null")
	nullNode := arrayNode.DescendantForByteRange(uint(nullIndex+1), uint(nullIndex+4))
	assert.NotNil(t, nullNode)
	assert.Equal(t, "null", nullNode.Kind())
	assert.EqualValues(t, nullIndex, nullNode.StartByte())
	assert.EqualValues(t, nullIndex+4, nullNode.EndByte())
	assert.Equal(t, Point{6, 9}, nullNode.StartPosition())
	assert.Equal(t, Point{6, 13}, nullNode.EndPosition())

	// Leaf node starts before the lower bound, ends at the upper bound - point query
	nullNode = arrayNode.DescendantForPointRange(Point{6, 11}, Point{6, 13})
	assert.NotNil(t, nullNode)
	assert.Equal(t, "null", nullNode.Kind())
	assert.EqualValues(t, nullIndex, nullNode.StartByte())
	assert.EqualValues(t, nullIndex+4, nullNode.EndByte())
	assert.Equal(t, Point{6, 9}, nullNode.StartPosition())
	assert.Equal(t, Point{6, 13}, nullNode.EndPosition())

	// The bounds span multiple leaf nodes - return the smallest node that does span it.
	pairNode := arrayNode.DescendantForByteRange(uint(stringIndex+2), uint(stringIndex+4))
	assert.NotNil(t, pairNode)
	assert.Equal(t, "pair", pairNode.Kind())
	assert.EqualValues(t, stringIndex, pairNode.StartByte())
	assert.EqualValues(t, stringIndex+9, pairNode.EndByte())
	assert.Equal(t, Point{6, 4}, pairNode.StartPosition())
	assert.Equal(t, Point{6, 13}, pairNode.EndPosition())

	assert.Equal(t, colonNode.Parent(), pairNode)

	// no leaf spans the given range - return the smallest node that does span it.
	pairNode = arrayNode.NamedDescendantForPointRange(Point{6, 6}, Point{6, 8})
	assert.NotNil(t, pairNode)
	assert.Equal(t, "pair", pairNode.Kind())
	assert.EqualValues(t, stringIndex, pairNode.StartByte())
	assert.EqualValues(t, stringIndex+9, pairNode.EndByte())
	assert.Equal(t, Point{6, 4}, pairNode.StartPosition())
	assert.Equal(t, Point{6, 13}, pairNode.EndPosition())

	// Negative test, start > end
	assert.Nil(t, arrayNode.DescendantForByteRange(1, 0))
	assert.Nil(t, arrayNode.DescendantForPointRange(Point{6, 8}, Point{6, 7}))
}

func TestNodeEdit(t *testing.T) {
	code := []byte(JSON_EXAMPLE)
	tree := parseJsonExample()
	rand := rand.New(rand.NewSource(0))

	for i := 0; i < 10; i++ {
		nodesBefore := getAllNodes(tree)

		edit := getRandomEdit(rand, code)
		tree2 := tree.Clone()
		edit2, err := performEdit(tree2, &code, &edit)
		assert.Nil(t, err)
		for i, node := range nodesBefore {
			node.Edit(&edit2)
			assert.Equal(t, node.Kind(), nodesBefore[i].Kind())
			assert.EqualValues(t, node.StartByte(), nodesBefore[i].StartByte())
			assert.Equal(t, node.StartPosition(), nodesBefore[i].StartPosition())
		}

		tree = tree2
	}
}

func TestRootNodeWithOffset(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse([]byte("  if (a) b"), nil)
	defer tree.Close()

	node := tree.RootNodeWithOffset(6, Point{2, 2})
	assert.NotNil(t, node)
	assert.EqualValues(t, 8, node.StartByte())
	assert.EqualValues(t, 16, node.EndByte())
	assert.Equal(t, Point{2, 4}, node.StartPosition())
	assert.Equal(t, Point{2, 12}, node.EndPosition())

	child := node.Child(0).Child(2)
	assert.Equal(t, "expression_statement", child.Kind())
	assert.EqualValues(t, 15, child.StartByte())
	assert.EqualValues(t, 16, child.EndByte())
	assert.Equal(t, Point{2, 11}, child.StartPosition())
	assert.Equal(t, Point{2, 12}, child.EndPosition())

	cursor := node.Walk()
	cursor.GotoFirstChild()
	cursor.GotoFirstChild()
	cursor.GotoNextSibling()
	child = cursor.Node()
	assert.Equal(t, "parenthesized_expression", child.Kind())
	assert.EqualValues(t, 11, child.StartByte())
	assert.EqualValues(t, 14, child.EndByte())
	assert.Equal(t, Point{2, 7}, child.StartPosition())
	assert.Equal(t, Point{2, 10}, child.EndPosition())
}

func TestNodeIsExtra(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse([]byte("foo(/* hi */);"), nil)
	defer tree.Close()

	rootNode := tree.RootNode()
	commentNode := rootNode.DescendantForByteRange(7, 7)

	assert.Equal(t, "program", rootNode.Kind())
	assert.Equal(t, "comment", commentNode.Kind())
	assert.False(t, rootNode.IsExtra())
	assert.True(t, commentNode.IsExtra())
}

func TestNodeIsError(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse([]byte("foo("), nil)
	defer tree.Close()

	rootNode := tree.RootNode()
	assert.Equal(t, "program", rootNode.Kind())
	assert.True(t, rootNode.HasError())

	child := rootNode.Child(0)
	assert.Equal(t, "ERROR", child.Kind())
	assert.True(t, child.IsError())
}

func TestNodeSexp(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	tree := parser.Parse([]byte("if (a) b"), nil)
	defer tree.Close()

	rootNode := tree.RootNode()
	ifNode := rootNode.DescendantForByteRange(0, 0)
	parenNode := rootNode.DescendantForByteRange(3, 3)
	identifierNode := rootNode.DescendantForByteRange(4, 4)

	assert.Equal(t, "if", ifNode.Kind())
	assert.Equal(t, "(\"if\")", ifNode.ToSexp())
	assert.Equal(t, "(", parenNode.Kind())
	assert.Equal(t, "(\"(\")", parenNode.ToSexp())
	assert.Equal(t, "identifier", identifierNode.Kind())
	assert.Equal(t, "(identifier)", identifierNode.ToSexp())
}

func TestNodeNumericSymbolsRespectSimpleAliases(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("python"))

	// Example 1:
	// Python argument lists can contain "splat" arguments, which are not allowed
	// within other expressions. This includes `parenthesized_list_splat` nodes
	// like `(*b)`. These `parenthesized_list_splat` nodes are aliased as
	// `parenthesized_expression`. Their numeric `symbol`, aka `kind_id` should
	// match that of a normal `parenthesized_expression`.
	tree := parser.Parse([]byte("(a((*b)))"), nil)
	defer tree.Close()
	root := tree.RootNode()
	assert.Equal(
		t,
		"(module (expression_statement (parenthesized_expression (call function: (identifier) arguments: (argument_list (parenthesized_expression (list_splat (identifier))))))))",
		root.ToSexp(),
	)

	outExprNode := root.Child(0).Child(0)
	assert.Equal(t, "parenthesized_expression", outExprNode.Kind())

	innerExprNode := outExprNode.NamedChild(0).ChildByFieldName("arguments").NamedChild(0)
	assert.Equal(t, "parenthesized_expression", innerExprNode.Kind())
	assert.Equal(t, outExprNode.KindId(), innerExprNode.KindId())

	// Example 2:
	// Ruby handles the unary (negative) and binary (minus) `-` operators using two
	// different tokens. One or more of these is an external token that's
	// aliased as `-`. Their numeric kind ids should match.
	parser.SetLanguage(getLanguage("ruby"))
	tree = parser.Parse([]byte("-a - b"), nil)
	root = tree.RootNode()
	assert.Equal(
		t,
		"(program (binary left: (unary operand: (identifier)) right: (identifier)))",
		root.ToSexp(),
	)

	binaryNode := root.Child(0)
	assert.Equal(t, "binary", binaryNode.Kind())

	unaryMinusNode := binaryNode.ChildByFieldName("left").Child(0)
	assert.Equal(t, "-", unaryMinusNode.Kind())

	binaryMinusNode := binaryNode.ChildByFieldName("operator")
	assert.Equal(t, "-", binaryMinusNode.Kind())
	assert.Equal(t, unaryMinusNode.KindId(), binaryMinusNode.KindId())
}

func TestHiddenZeroWidthNodeWithVisibleChild(t *testing.T) {
	code := `
class Foo {
  std::
private:
  std::string s;
};
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("cpp"))
	tree := parser.Parse([]byte(code), nil)
	defer tree.Close()
	root := tree.RootNode()

	classSpecifier := root.Child(0)
	fieldDeclList := classSpecifier.ChildByFieldName("body")
	fieldDecl := fieldDeclList.NamedChild(0)
	fieldIdent := fieldDecl.ChildByFieldName("declarator")
	assert.Equal(t, fieldIdent, fieldDecl.ChildWithDescendant(fieldIdent))
}

func getAllNodes(tree *Tree) []*Node {
	var result []*Node
	visitedChildren := false
	cursor := tree.Walk()
	for {
		if !visitedChildren {
			result = append(result, cursor.Node())
			if !cursor.GotoFirstChild() {
				visitedChildren = true
			}
		} else if cursor.GotoNextSibling() {
			visitedChildren = false
		} else if !cursor.GotoParent() {
			break
		}
	}
	return result
}

func parseJsonExample() *Tree {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("json"))
	return parser.Parse([]byte(JSON_EXAMPLE), nil)
}
