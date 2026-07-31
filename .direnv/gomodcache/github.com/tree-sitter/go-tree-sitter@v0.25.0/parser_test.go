package tree_sitter_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/stretchr/testify/assert"
	. "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_embedded_template "github.com/tree-sitter/tree-sitter-embedded-template/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_html "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

func getLanguage(name string) *Language {
	switch name {
	case "c":
		return NewLanguage(tree_sitter_c.Language())
	case "cpp":
		return NewLanguage(tree_sitter_cpp.Language())
	case "embedded-template":
		return NewLanguage(tree_sitter_embedded_template.Language())
	case "go":
		return NewLanguage(tree_sitter_go.Language())
	case "html":
		return NewLanguage(tree_sitter_html.Language())
	case "java":
		return NewLanguage(tree_sitter_java.Language())
	case "javascript":
		return NewLanguage(tree_sitter_javascript.Language())
	case "json":
		return NewLanguage(tree_sitter_json.Language())
	case "php":
		return NewLanguage(tree_sitter_php.LanguagePHP())
	case "python":
		return NewLanguage(tree_sitter_python.Language())
	case "ruby":
		return NewLanguage(tree_sitter_ruby.Language())
	case "rust":
		return NewLanguage(tree_sitter_rust.Language())
	default:
		return nil
	}
}

func ExampleParser_Parse() {
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
	fmt.Println(rootNode.ToSexp())
	// Output:
	// (source_file (package_clause (package_identifier)) (function_declaration name: (identifier) parameters: (parameter_list) body: (block (return_statement))))
}

func ExampleParser_ParseWithOptions() {
	parser := NewParser()
	defer parser.Close()

	language := NewLanguage(tree_sitter_go.Language())

	parser.SetLanguage(language)

	sourceCode := []byte(`
			package main

			func main() {
				return
			}
	`)

	readCallback := func(offset int, position Point) []byte {
		return sourceCode[offset:]
	}

	tree := parser.ParseWithOptions(readCallback, nil, nil)
	defer tree.Close()

	rootNode := tree.RootNode()
	fmt.Println(rootNode.ToSexp())
	// Output:
	// (source_file (package_clause (package_identifier)) (function_declaration name: (identifier) parameters: (parameter_list) body: (block (return_statement))))
}

func TestParsingSimpleString(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	tree := parser.Parse([]byte(`
        struct Stuff {}
        fn main() {}
    `), nil)

	rootNode := tree.RootNode()
	assert.Equal(t, rootNode.Kind(), "source_file")

	assert.Equal(t, rootNode.ToSexp(), "(source_file (struct_item name: (type_identifier) body: (field_declaration_list)) (function_item name: (identifier) parameters: (parameters) body: (block)))")

	structNode := rootNode.Child(0)
	assert.Equal(t, structNode.Kind(), "struct_item")
}

func TestParsingWithLogging(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	messages := []struct {
		string
		LogType
	}{}
	parser.SetLogger(func(logType LogType, message string) {
		messages = append(messages, struct {
			string
			LogType
		}{message, logType})
	})

	parser.Parse([]byte(`
        struct Stuff {}
        fn main() {}
	`), nil)

	assert.Contains(t, messages, struct {
		string
		LogType
	}{"reduce sym:struct_item, child_count:3", LogTypeParse})
	assert.Contains(t, messages, struct {
		string
		LogType
	}{"skip character:' '", LogTypeLex})

	rowStartsFrom0 := false
	for _, m := range messages {
		if strings.Contains(m.string, "row:0") {
			rowStartsFrom0 = true
			break
		}
	}

	assert.True(t, rowStartsFrom0)
}

func TestParsingWithDebugGraphEnabled(t *testing.T) {
	hasZeroIndexedRow := func(s string) bool {
		return strings.Contains(s, "position: 0,")
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	debugGraphFile, err := os.CreateTemp("", ".tree-sitter-test-debug-graph")
	assert.Nil(t, err)

	parser.PrintDotGraphs(debugGraphFile)
	parser.Parse([]byte("const zero = 0"), nil)

	debugGraphFile.Seek(0, 0)
	logReader := bufio.NewReader(debugGraphFile)
	for {
		line, err := logReader.ReadString('\n')
		if err != nil {
			break
		}
		assert.False(t, hasZeroIndexedRow(line), "Graph log output includes zero-indexed row: %s", line)
	}

	debugGraphFile.Close()
	os.Remove(debugGraphFile.Name())
}

func TestParsingWithCustomUTF8Input(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	lines := []string{"pub fn foo() {", "  1", "}"}

	tree := parser.ParseWithOptions(func(_ int, position Point) []byte {
		row := position.Row
		column := position.Column
		if row < uint(len(lines)) {
			if column < uint(len(lines[row])) {
				return []byte(lines[row][column:])
			} else {
				return []byte("\n")
			}
		} else {
			return []byte{}
		}
	}, nil, nil)

	root := tree.RootNode()
	assert.Equal(t, root.ToSexp(), "(source_file (function_item (visibility_modifier) name: (identifier) parameters: (parameters) body: (block (integer_literal))))")
	assert.Equal(t, root.Kind(), "source_file")
	assert.False(t, root.HasError())
	assert.Equal(t, root.Child(0).Kind(), "function_item")
}

func TestParsingWithCustomUTF16LEInput(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	lines := [][]uint16{
		utf16.Encode([]rune("pub fn foo() {")),
		utf16.Encode([]rune("  1")),
		utf16.Encode([]rune("}")),
	}

	newline := []uint16{binary.LittleEndian.Uint16([]byte{'\n', 0})}

	tree := parser.ParseUTF16LEWithOptions(func(_ int, position Point) []uint16 {
		row := position.Row
		column := position.Column
		if row < uint(len(lines)) {
			if column < uint(len(lines[row])) {
				return lines[row][column:]
			} else {
				return newline
			}
		} else {
			return []uint16{}
		}
	}, nil, nil)

	root := tree.RootNode()
	assert.Equal(t, root.ToSexp(), "(source_file (function_item (visibility_modifier) name: (identifier) parameters: (parameters) body: (block (integer_literal))))")
	assert.Equal(t, root.Kind(), "source_file")
	assert.False(t, root.HasError())
	assert.Equal(t, root.Child(0).Kind(), "function_item")
}

func TestParsingWithCustomUTF16BEInput(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	lines := [][]uint16{
		utf16.Encode([]rune("pub fn foo() {")),
		utf16.Encode([]rune("  1")),
		utf16.Encode([]rune("}")),
	}

	i := 1
	bigEndian := (*[int(unsafe.Sizeof(0))]byte)(unsafe.Pointer(&i))[0] == 0

	if !bigEndian {
		for _, line := range lines {
			for i := 0; i < len(line); i++ {
				line[i] = line[i]<<8 | line[i]>>8
			}
		}
	}

	newline := []uint16{binary.BigEndian.Uint16([]byte{'\n', 0})}

	tree := parser.ParseUTF16BEWithOptions(func(_ int, position Point) []uint16 {
		row := position.Row
		column := position.Column
		if row < uint(len(lines)) {
			if column < uint(len(lines[row])) {
				return lines[row][column:]
			} else {
				return newline
			}
		} else {
			return []uint16{}
		}
	}, nil, nil)

	root := tree.RootNode()
	assert.Equal(t, root.ToSexp(), "(source_file (function_item (visibility_modifier) name: (identifier) parameters: (parameters) body: (block (integer_literal))))")
	assert.Equal(t, root.Kind(), "source_file")
	assert.False(t, root.HasError())
	assert.Equal(t, root.Child(0).Kind(), "function_item")
}

func TestParsingWithCallbackReturningOwnedStrings(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	text := []byte("pub fn foo() { 1 }")

	tree := parser.ParseWithOptions(func(i int, _ Point) []byte {
		return text[i:]
	}, nil, nil)

	root := tree.RootNode()
	assert.Equal(t, root.ToSexp(), "(source_file (function_item (visibility_modifier) name: (identifier) parameters: (parameters) body: (block (integer_literal))))")
}

func TestParsingTextWithByteOrderMark(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	// Parse UTF16 text with a BOM
	tree := parser.ParseUTF16LE([]uint16{0xFEFF, 'f', 'n', ' ', 'a', '(', ')', ' ', '{', '}'}, nil)
	assert.Equal(t, tree.RootNode().ToSexp(), "(source_file (function_item name: (identifier) parameters: (parameters) body: (block)))")
	assert.Equal(t, tree.RootNode().StartByte(), uint(2))

	// Parse UTF8 text with a BOM
	tree = parser.Parse([]byte("\xEF\xBB\xBFfn a() {}"), nil)
	assert.Equal(t, tree.RootNode().ToSexp(), "(source_file (function_item name: (identifier) parameters: (parameters) body: (block)))")
	assert.Equal(t, tree.RootNode().StartByte(), uint(3))

	// Edit the text, inserting a character before the BOM. The BOM is now an error.
	tree.Edit(&InputEdit{
		StartByte:      0,
		OldEndByte:     0,
		NewEndByte:     1,
		StartPosition:  Point{0, 0},
		OldEndPosition: Point{0, 0},
		NewEndPosition: Point{0, 1},
	})
	tree = parser.Parse([]byte{' ', 0xEF, 0xBB, 0xBF, 'f', 'n', ' ', 'a', '(', ')', ' ', '{', '}'}, tree)
	assert.Equal(t, tree.RootNode().ToSexp(), "(source_file (ERROR (UNEXPECTED 65279)) (function_item name: (identifier) parameters: (parameters) body: (block)))")
	assert.Equal(t, tree.RootNode().StartByte(), uint(1))

	// Edit the text again, putting the BOM back at the beginning.
	tree.Edit(&InputEdit{
		StartByte:      0,
		OldEndByte:     1,
		NewEndByte:     0,
		StartPosition:  Point{0, 0},
		OldEndPosition: Point{0, 1},
		NewEndPosition: Point{0, 0},
	})
	tree = parser.Parse([]byte{0xEF, 0xBB, 0xBF, 'f', 'n', ' ', 'a', '(', ')', ' ', '{', '}'}, tree)
	assert.Equal(t, tree.RootNode().ToSexp(), "(source_file (function_item name: (identifier) parameters: (parameters) body: (block)))")
	assert.Equal(t, tree.RootNode().StartByte(), uint(3))
}

func TestParsingInvalidCharsAtEOF(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("json"))

	tree := parser.Parse([]byte("\xdf"), nil)
	defer tree.Close()
	assert.Equal(t, tree.RootNode().ToSexp(), "(document (ERROR (UNEXPECTED INVALID)))")
}

func TestParsingUnexpectedNullCharactersWithinSource(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	tree := parser.Parse([]byte("var \x00 something;"), nil)
	defer tree.Close()
	assert.Equal(t, tree.RootNode().ToSexp(), "(program (variable_declaration (ERROR (UNEXPECTED '\\0')) (variable_declarator name: (identifier))))")
}

func TestParsingEndsWhenInputCallbackReturnsEmpty(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	source := []byte("abcdefghijklmnoqrs")
	tree := parser.ParseWithOptions(func(offset int, _ Point) []byte {
		if offset >= 6 {
			return []byte{}
		} else {
			return source[offset:int(math.Min(float64(len(source)), float64(offset+3)))]
		}
	}, nil, nil)

	assert.Equal(t, tree.RootNode().EndByte(), uint(6))
}

func TestParsingAfterEditingBeginningOfCode(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	code := []byte("123 + 456 * (10 + x);")
	tree := parser.Parse(code, nil)
	defer tree.Close()
	assert.Equal(
		t,
		"(program (expression_statement (binary_expression "+
			"left: (number) "+
			"right: (binary_expression left: (number) right: (parenthesized_expression "+
			"(binary_expression left: (number) right: (identifier)))))))",
		tree.RootNode().ToSexp(),
	)

	performEdit(
		tree,
		&code,
		&testEdit{
			position:      3,
			deletedLength: 0,
			insertedText:  []byte(" || 5"),
		},
	)

	recorder := newReadRecorder(code)
	tree = parser.ParseWithOptions(func(i int, _ Point) []byte {
		return recorder.Read(i)
	}, tree, nil)
	assert.Equal(
		t,
		"(program (expression_statement (binary_expression "+
			"left: (number) "+
			"right: (binary_expression "+
			"left: (number) "+
			"right: (binary_expression "+
			"left: (number) "+
			"right: (parenthesized_expression (binary_expression left: (number) right: (identifier))))))))",
		tree.RootNode().ToSexp(),
	)

	assert.Equal(t, []string{"123 || 5 "}, recorder.StringsRead())
}

func TestParsingAfterEditingEndOfCode(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	code := []byte("x * (100 + abc);")
	tree := parser.Parse(code, nil)
	defer tree.Close()
	assert.Equal(
		t,
		"(program (expression_statement (binary_expression "+
			"left: (identifier) "+
			"right: (parenthesized_expression (binary_expression left: (number) right: (identifier))))))",
		tree.RootNode().ToSexp(),
	)

	position := uint(len(code) - 2)
	performEdit(
		tree,
		&code,
		&testEdit{
			position:      position,
			deletedLength: 0,
			insertedText:  []byte(".d"),
		},
	)

	recorder := newReadRecorder(code)
	tree = parser.ParseWithOptions(func(i int, _ Point) []byte {
		return recorder.Read(i)
	}, tree, nil)
	assert.Equal(
		t,
		"(program (expression_statement (binary_expression "+
			"left: (identifier) "+
			"right: (parenthesized_expression (binary_expression "+
			"left: (number) "+
			"right: (member_expression "+
			"object: (identifier) "+
			"property: (property_identifier)))))))",
		tree.RootNode().ToSexp(),
	)

	assert.Equal(t, []string{" * ", "abc.d)"}, recorder.StringsRead())
}

func TestParsingEmptyFileWithReusedTree(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	tree := parser.Parse([]byte(""), nil)
	defer tree.Close()
	parser.Parse([]byte(""), tree)

	tree = parser.Parse([]byte("\n  "), nil)
	parser.Parse([]byte("\n  "), tree)
}

func TestParsingAfterDetectingErrorInTheMiddleOfStringToken(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("python"))

	source := []byte("a = b, 'c, d'")
	tree := parser.Parse(source, nil)
	defer tree.Close()
	assert.Equal(
		t,
		"(module (expression_statement (assignment left: (identifier) right: (expression_list (identifier) (string (string_start) (string_content) (string_end))))))",
		tree.RootNode().ToSexp(),
	)

	editIx := strings.Index(string(source), "d'")
	edit := &testEdit{
		position:      uint(editIx),
		deletedLength: uint(len(source) - editIx),
		insertedText:  []byte{},
	}
	undo := invertEdit(source, edit)

	tree2 := tree.Clone()
	performEdit(tree2, &source, edit)
	tree2 = parser.Parse(source, tree2)
	assert.True(t, tree2.RootNode().HasError())

	tree3 := tree2.Clone()
	performEdit(tree3, &source, undo)
	tree3 = parser.Parse(source, tree3)
	assert.Equal(t, tree3.RootNode().ToSexp(), tree.RootNode().ToSexp())
}

func TestParsingOnMultipleThreads(t *testing.T) {
	// Parse this source file so that each thread has a non-trivial amount of
	// work to do.
	thisFileSource, err := os.ReadFile("tree-sitter/cli/src/tests/parser_test.rs")
	assert.Nil(t, err)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))
	tree := parser.Parse(thisFileSource, nil)
	defer tree.Close()

	parseThreads := make([]chan *Tree, 4)

	for threadId := 1; threadId < 5; threadId++ {
		parseThreads[threadId-1] = make(chan *Tree)
		treeClone := tree.Clone()
		go func(threadId int, tree *Tree) {
			// For each thread, prepend a different number of declarations to the
			// source code.
			prependLineCount := 0
			prependedSource := ""
			for i := 0; i < threadId; i++ {
				prependLineCount += 2
				prependedSource += "struct X {}\n\n"
			}

			treeClone.Edit(&InputEdit{
				StartByte:      0,
				OldEndByte:     0,
				NewEndByte:     uint(len(prependedSource)),
				StartPosition:  Point{0, 0},
				OldEndPosition: Point{0, 0},
				NewEndPosition: Point{uint(prependLineCount), 0},
			})

			prependedSource += string(thisFileSource)

			// Reparse using the old tree as a starting point.
			parser := NewParser()
			defer parser.Close()
			parser.SetLanguage(getLanguage("rust"))
			parseThreads[threadId-1] <- parser.Parse([]byte(prependedSource), treeClone)
		}(threadId, treeClone)
	}

	// Check that the trees have the expected relationship to one another.
	childCountDifferences := make([]int, 4)

	for i := 0; i < 4; i++ {
		treeClone := <-parseThreads[i]
		childCountDifferences[i] = int(treeClone.RootNode().ChildCount() - tree.RootNode().ChildCount())
	}

	assert.Equal(t, []int{1, 2, 3, 4}, childCountDifferences)
}

func TestParsingCancelledByAnotherThread(t *testing.T) {
	var cancellationFlag atomic.Value
	flag := uintptr(0)
	cancellationFlag.Store(&flag)
	callback := func(ParseState) bool {
		return atomic.LoadUintptr(cancellationFlag.Load().(*uintptr)) != 0
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	// Long input - parsing succeeds
	tree := parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			if offset == 0 {
				return []byte(" [")
			} else if offset >= 20000 {
				return []byte{}
			} else {
				return []byte("0,")
			}
		},
		nil,
		&ParseOptions{ProgressCallback: callback},
	)
	assert.NotNil(t, tree)

	cancelThread := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		atomic.StoreUintptr(cancellationFlag.Load().(*uintptr), 1)
		close(cancelThread)
	}()

	// Infinite input
	tree = parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			runtime.Gosched()
			time.Sleep(10 * time.Millisecond)
			if offset == 0 {
				return []byte(" [")
			} else {
				return []byte("0,")
			}
		},
		nil,
		&ParseOptions{ProgressCallback: callback},
	)

	<-cancelThread
	assert.Nil(t, tree)
}

func TestParsingWithTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to millisecond timer resolution limitations")
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("json"))

	// Parse an infinitely-long array, but pause after 1ms of processing.
	startTime := time.Now()
	tree := parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			if offset == 0 {
				return []byte(" [")
			} else {
				return []byte(",0")
			}
		},
		nil,
		&ParseOptions{ProgressCallback: func(ParseState) bool {
			return time.Since(startTime) > 1000*time.Microsecond
		}},
	)
	assert.Nil(t, tree)
	assert.True(t, time.Since(startTime) < 2000*time.Microsecond)

	// Continue parsing, but pause after 5 ms of processing.
	startTime = time.Now()
	tree = parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			if offset == 0 {
				return []byte(" [")
			} else {
				return []byte(",0")
			}
		},
		nil,
		&ParseOptions{ProgressCallback: func(ParseState) bool {
			return time.Since(startTime) > 5000*time.Microsecond
		}},
	)
	assert.Nil(t, tree)
	assert.True(t, time.Since(startTime) > 100*time.Microsecond)
	assert.True(t, time.Since(startTime) < 10000*time.Microsecond)

	// Finish parsing
	tree = parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			if offset >= 5001 {
				return []byte{}
			} else if offset == 5000 {
				return []byte("]")
			} else {
				return []byte(",0")
			}
		},
		nil,
		nil,
	)
	assert.Equal(t, "array", tree.RootNode().Child(0).Kind())
}

func TestParsingWithTimeoutAndReset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to millisecond timer resolution limitations")
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("json"))

	start := time.Now()
	code := []byte("[\"ok\", 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32]")
	callback := func(offset int, _ Point) []byte {
		if offset >= len(code) {
			return []byte{}
		} else {
			return code[offset:]
		}
	}
	tree := parser.ParseWithOptions(
		callback,
		nil,
		&ParseOptions{ProgressCallback: func(ParseState) bool {
			return time.Since(start) > 2*time.Microsecond
		}},
	)

	defer tree.Close()
	assert.Nil(t, tree)

	// Without calling reset, the parser continues from where it left off, so
	// it does not see the changes to the beginning of the source code.
	code = []byte("[null, 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32]")
	tree = parser.ParseWithOptions(callback, nil, nil)
	assert.Equal(t, "string", tree.RootNode().NamedChild(0).NamedChild(0).Kind())

	code = []byte("[\"ok\", 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32]")
	tree = parser.ParseWithOptions(
		callback,
		nil,
		&ParseOptions{ProgressCallback: func(ParseState) bool {
			return time.Since(start) > 2*time.Microsecond
		}},
	)
	assert.Nil(t, tree)

	// By calling reset, we force the parser to start over from scratch so
	// that it sees the changes to the beginning of the source code.
	parser.Reset()
	code = []byte("[null, 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32]")
	tree = parser.ParseWithOptions(callback, nil, nil)
	assert.Equal(t, "null", tree.RootNode().NamedChild(0).NamedChild(0).Kind())
}

func TestParsingWithTimeoutAndImplicitReset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to millisecond timer resolution limitations")
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	start := time.Now()
	code := []byte("[\"ok\", 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32]")
	callback := func(offset int, _ Point) []byte {
		if offset >= len(code) {
			return []byte{}
		} else {
			return code[offset:]
		}
	}
	tree := parser.ParseWithOptions(
		callback,
		nil,
		&ParseOptions{ProgressCallback: func(ParseState) bool {
			return time.Since(start) > 5*time.Microsecond
		}},
	)
	defer tree.Close()
	assert.Nil(t, tree)

	// Changing the parser's language implicitly resets, discarding the previous partial parse.
	parser.SetLanguage(getLanguage("json"))
	code = []byte("[null, 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32]")
	tree = parser.ParseWithOptions(callback, nil, nil)
	assert.Equal(t, "null", tree.RootNode().NamedChild(0).NamedChild(0).Kind())
}

func TestParsingWithTimeoutAndNoCompletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to millisecond timer resolution limitations")
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	start := time.Now()
	code := []byte("[\"ok\", 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32]")
	callback := func(offset int, _ Point) []byte {
		if offset >= len(code) {
			return []byte{}
		} else {
			return code[offset:]
		}
	}
	tree := parser.ParseWithOptions(
		callback,
		nil,
		&ParseOptions{ProgressCallback: func(ParseState) bool {
			return time.Since(start) > 5*time.Microsecond
		}},
	)
	defer tree.Close()
	assert.Nil(t, tree)
}

func TestParsingWithTimeoutDuringBalancing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to millisecond timer resolution limitations")
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	functionCount := 100

	code := strings.Repeat("function() {}\n", functionCount)
	currentByteOffset := uint32(0)
	inBalancing := false
	tree := parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			if offset >= len(code) {
				return []byte{}
			} else {
				return []byte(code[offset:])
			}
		},
		nil,
		&ParseOptions{ProgressCallback: func(state ParseState) bool {
			// The parser will call the progress_callback during parsing, and at the very end
			// during tree-balancing. For very large trees, this balancing act can take quite
			// some time, so we want to verify that timing out during this operation is
			// possible.
			//
			// We verify this by checking the current byte offset, as this number will *not* be
			// updated during tree balancing. If we see the same offset twice, we know that we
			// are in the balancing phase.
			if state.CurrentByteOffset != currentByteOffset {
				currentByteOffset = state.CurrentByteOffset
				return false
			} else {
				inBalancing = true
				return true
			}
		}},
	)

	assert.Nil(t, tree)
	assert.True(t, inBalancing)

	// If we resume parsing (implying we didn't call `parser.reset()`), we should be able to
	// finish parsing the tree, continuing from where we left off.
	tree = parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			if offset >= len(code) {
				return []byte{}
			} else {
				return []byte(code[offset:])
			}
		},
		nil,
		&ParseOptions{ProgressCallback: func(state ParseState) bool {
			// Because we've already finished parsing, we should only be resuming the
			// balancing phase.
			assert.Equal(t, currentByteOffset, state.CurrentByteOffset)
			return false
		}},
	)

	assert.False(t, tree.RootNode().HasError())
	assert.EqualValues(t, functionCount, tree.RootNode().ChildCount())
}

func TestParsingWithTimeoutWhenErrorDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to millisecond timer resolution limitations")
	}

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("json"))

	// Parse an infinitely-long array, but insert an error after 1000 characters.
	offset := uint32(0)
	erroneousCode := "!,"
	tree := parser.ParseWithOptions(
		func(i int, _ Point) []byte {
			if i == 0 {
				return []byte("[")
			} else if i <= 1000 {
				return []byte("0,")
			} else {
				return []byte(erroneousCode)
			}
		},
		nil,
		&ParseOptions{ProgressCallback: func(state ParseState) bool {
			offset = state.CurrentByteOffset
			return state.HasError
		}},
	)

	// The callback is called at the end of parsing, however, what we're asserting here is that
	// parsing ends immediately as the error is detected. This is verified by checking the offset
	// of the last byte processed is the length of the erroneous code we inserted, aka, 1002, or
	// 1000 + the length of the erroneous code.
	assert.EqualValues(t, 1000+len(erroneousCode), offset)
	assert.Nil(t, tree)
}

// Included Ranges

func TestParsingWithOneIncludedRange(t *testing.T) {
	sourceCode := "<span>hi</span><script>console.log('sup');</script>"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("html"))
	htmlTree := parser.Parse([]byte(sourceCode), nil)
	scriptContentNode := htmlTree.RootNode().Child(1).Child(1)
	assert.Equal(t, "raw_text", scriptContentNode.Kind())

	assert.Equal(t, []Range{
		{
			StartByte:  0,
			EndByte:    math.MaxUint32,
			StartPoint: Point{0, 0},
			EndPoint:   Point{math.MaxUint32, math.MaxUint32},
		},
	}, parser.IncludedRanges())

	parser.SetIncludedRanges([]Range{scriptContentNode.Range()})
	assert.Equal(t, []Range{scriptContentNode.Range()}, parser.IncludedRanges())

	parser.SetLanguage(getLanguage("javascript"))
	jsTree := parser.Parse([]byte(sourceCode), nil)

	assert.Equal(
		t,
		"(program (expression_statement (call_expression function: (member_expression object: (identifier) property: (property_identifier)) arguments: (arguments (string (string_fragment))))))",
		jsTree.RootNode().ToSexp(),
	)
	assert.Equal(t, Point{0, uint(strings.Index(sourceCode, "console"))}, jsTree.RootNode().StartPosition())
	assert.Equal(t, []Range{scriptContentNode.Range()}, jsTree.IncludedRanges())
}

func TestParsingWithMultipleIncludedRanges(t *testing.T) {
	sourceCode := "html `<div>Hello, ${name.toUpperCase()}, it's <b>${now()}</b>.</div>`"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	jsTree := parser.Parse([]byte(sourceCode), nil)
	templateStringNode := jsTree.RootNode().DescendantForByteRange(
		uint(strings.Index(sourceCode, "`<")),
		uint(strings.Index(sourceCode, ">`")),
	)
	assert.Equal(t, "template_string", templateStringNode.Kind())

	openQuoteNode := templateStringNode.Child(0)
	interpolationNode1 := templateStringNode.Child(2)
	interpolationNode2 := templateStringNode.Child(4)
	closeQuoteNode := templateStringNode.Child(6)

	parser.SetLanguage(getLanguage("html"))
	htmlRanges := []Range{
		{
			StartByte:  openQuoteNode.EndByte(),
			StartPoint: openQuoteNode.EndPosition(),
			EndByte:    interpolationNode1.StartByte(),
			EndPoint:   interpolationNode1.StartPosition(),
		},
		{
			StartByte:  interpolationNode1.EndByte(),
			StartPoint: interpolationNode1.EndPosition(),
			EndByte:    interpolationNode2.StartByte(),
			EndPoint:   interpolationNode2.StartPosition(),
		},
		{
			StartByte:  interpolationNode2.EndByte(),
			StartPoint: interpolationNode2.EndPosition(),
			EndByte:    closeQuoteNode.StartByte(),
			EndPoint:   closeQuoteNode.StartPosition(),
		},
	}
	parser.SetIncludedRanges(htmlRanges)
	htmlTree := parser.Parse([]byte(sourceCode), nil)

	assert.Equal(
		t,
		"(document (element (start_tag (tag_name)) (text) (element (start_tag (tag_name)) (end_tag (tag_name))) (text) (end_tag (tag_name))))",
		htmlTree.RootNode().ToSexp(),
	)
	assert.Equal(t, htmlRanges, htmlTree.IncludedRanges())

	divElementNode := htmlTree.RootNode().Child(0)
	helloTextNode := divElementNode.Child(1)
	bElementNode := divElementNode.Child(2)
	bStartTagNode := bElementNode.Child(0)
	bEndTagNode := bElementNode.Child(1)

	assert.Equal(t, "text", helloTextNode.Kind())
	assert.Equal(t, uint(strings.Index(sourceCode, "Hello")), helloTextNode.StartByte())
	assert.Equal(t, uint(strings.Index(sourceCode, " <b>")), helloTextNode.EndByte())

	assert.Equal(t, "start_tag", bStartTagNode.Kind())
	assert.Equal(t, uint(strings.Index(sourceCode, "<b>")), bStartTagNode.StartByte())
	assert.Equal(t, uint(strings.Index(sourceCode, "${now()}")), bStartTagNode.EndByte())

	assert.Equal(t, "end_tag", bEndTagNode.Kind())
	assert.Equal(t, uint(strings.Index(sourceCode, "</b>")), bEndTagNode.StartByte())
	assert.Equal(t, uint(strings.Index(sourceCode, ".</div>")), bEndTagNode.EndByte())
}

func TestParsingWithIncludedRangeContainingMismatchedPositions(t *testing.T) {
	sourceCode := "<div>test</div>{_ignore_this_part_}"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("html"))

	endByte := strings.Index(sourceCode, "{_ignore_this_part_")

	rangeToParse := Range{
		StartByte:  0,
		StartPoint: Point{10, 12},
		EndByte:    uint(endByte),
		EndPoint:   Point{10, uint(12 + endByte)},
	}

	parser.SetIncludedRanges([]Range{rangeToParse})

	htmlTree := parser.ParseWithOptions(chunkedInput(sourceCode, 3), nil, nil)

	assert.Equal(t, rangeToParse, htmlTree.RootNode().Range())
	assert.Equal(
		t,
		"(document (element (start_tag (tag_name)) (text) (end_tag (tag_name))))",
		htmlTree.RootNode().ToSexp(),
	)
}

func TestParsingErrorInInvalidIncludedRanges(t *testing.T) {
	parser := NewParser()
	defer parser.Close()

	// Ranges are not ordered
	err := parser.SetIncludedRanges([]Range{
		{
			StartByte:  23,
			EndByte:    29,
			StartPoint: Point{0, 23},
			EndPoint:   Point{0, 29},
		},
		{
			StartByte:  0,
			EndByte:    5,
			StartPoint: Point{0, 0},
			EndPoint:   Point{0, 5},
		},
		{
			StartByte:  50,
			EndByte:    60,
			StartPoint: Point{0, 50},
			EndPoint:   Point{0, 60},
		},
	})
	assert.Equal(t, &IncludedRangesError{1}, err)

	// Range ends before it starts
	err = parser.SetIncludedRanges([]Range{
		{
			StartByte:  10,
			EndByte:    5,
			StartPoint: Point{0, 10},
			EndPoint:   Point{0, 5},
		},
	})
	assert.Equal(t, &IncludedRangesError{0}, err)
}

func TestParsingUTF16CodeWithErrorsAtEndOfIncludedRange(t *testing.T) {
	sourceCode := "<script>a.</script>"
	utf16SourceCode := utf16.Encode([]rune(sourceCode))

	startByte := 2 * strings.Index(sourceCode, "a.")
	endByte := 2 * strings.Index(sourceCode, "</script>")

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	parser.SetIncludedRanges([]Range{
		{
			StartByte:  uint(startByte),
			EndByte:    uint(endByte),
			StartPoint: Point{0, uint(startByte)},
			EndPoint:   Point{0, uint(endByte)},
		},
	})

	tree := parser.ParseUTF16LE(utf16SourceCode, nil)
	assert.Equal(t, "(program (ERROR (identifier)))", tree.RootNode().ToSexp())
}

func TestParsingWithExternalScannerThatUsesIncludedRangeBoundaries(t *testing.T) {
	sourceCode := "a <%= b() %> c <% d() %>"
	range1StartByte := strings.Index(sourceCode, " b() ")
	range1EndByte := range1StartByte + len(" b() ")
	range2StartByte := strings.Index(sourceCode, " d() ")
	range2EndByte := range2StartByte + len(" d() ")

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	parser.SetIncludedRanges([]Range{
		{
			StartByte:  uint(range1StartByte),
			EndByte:    uint(range1EndByte),
			StartPoint: Point{0, uint(range1StartByte)},
			EndPoint:   Point{0, uint(range1EndByte)},
		},
		{
			StartByte:  uint(range2StartByte),
			EndByte:    uint(range2EndByte),
			StartPoint: Point{0, uint(range2StartByte)},
			EndPoint:   Point{0, uint(range2EndByte)},
		},
	})

	tree := parser.Parse([]byte(sourceCode), nil)
	defer tree.Close()
	root := tree.RootNode()
	statement1 := root.Child(0)
	statement2 := root.Child(1)

	assert.Equal(
		t,
		"(program (expression_statement (call_expression function: (identifier) arguments: (arguments))) (expression_statement (call_expression function: (identifier) arguments: (arguments))))",
		root.ToSexp(),
	)

	assert.Equal(t, uint(strings.Index(sourceCode, "b()")), statement1.StartByte())
	assert.Equal(t, uint(strings.Index(sourceCode, " %> c")), statement1.EndByte())
	assert.Equal(t, uint(strings.Index(sourceCode, "d()")), statement2.StartByte())
	assert.Equal(t, uint(len(sourceCode)-len(" %>")), statement2.EndByte())
}

func TestParsingWithANewlyExcludedRange(t *testing.T) {
	sourceCode := "<div><span><%= something %></span></div>"

	// Parse HTML including the template directive, which will cause an error
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("html"))
	firstTree := parser.ParseWithOptions(chunkedInput(sourceCode, 3), nil, nil)

	// Insert code at the beginning of the document.
	prefix := "a very very long line of plain text. "
	firstTree.Edit(&InputEdit{
		StartByte:      0,
		OldEndByte:     0,
		NewEndByte:     uint(len(prefix)),
		StartPosition:  Point{0, 0},
		OldEndPosition: Point{0, 0},
		NewEndPosition: Point{0, uint(len(prefix))},
	})
	sourceCode = prefix + sourceCode

	// Parse the HTML again, this time *excluding* the template directive
	// (which has moved since the previous parse).
	directiveStart := strings.Index(sourceCode, "<%=")
	directiveEnd := strings.Index(sourceCode, "</span>")
	sourceCodeEnd := len(sourceCode)

	parser.SetIncludedRanges([]Range{
		{
			StartByte:  0,
			EndByte:    uint(directiveStart),
			StartPoint: Point{0, 0},
			EndPoint:   Point{0, uint(directiveStart)},
		},
		{
			StartByte:  uint(directiveEnd),
			EndByte:    uint(sourceCodeEnd),
			StartPoint: Point{0, uint(directiveEnd)},
			EndPoint:   Point{0, uint(sourceCodeEnd)},
		},
	})

	tree := parser.ParseWithOptions(chunkedInput(sourceCode, 3), firstTree, nil)

	assert.Equal(
		t,
		"(document (text) (element (start_tag (tag_name)) (element (start_tag (tag_name)) (end_tag (tag_name))) (end_tag (tag_name))))",
		tree.RootNode().ToSexp(),
	)

	assert.Equal(
		t,
		[]Range{
			// The first range that has changed syntax is the range of the newly-inserted text.
			{
				StartByte:  0,
				EndByte:    uint(len(prefix)),
				StartPoint: Point{0, 0},
				EndPoint:   Point{0, uint(len(prefix))},
			},
			// Even though no edits were applied to the outer `div` element,
			// its contents have changed syntax because a range of text that
			// was previously included is now excluded.
			{
				StartByte:  uint(directiveStart),
				EndByte:    uint(directiveEnd),
				StartPoint: Point{0, uint(directiveStart)},
				EndPoint:   Point{0, uint(directiveEnd)},
			},
		},
		tree.ChangedRanges(firstTree),
	)
}

func TestParsingWithANewlyIncludedRange(t *testing.T) {
	sourceCode := "<div><%= foo() %></div><span><%= bar() %></span><%= baz() %>"
	range1Start := strings.Index(sourceCode, " foo")
	range2Start := strings.Index(sourceCode, " bar")
	range3Start := strings.Index(sourceCode, " baz")
	range1End := range1Start + 7
	range2End := range2Start + 7
	range3End := range3Start + 7

	// Parse only the first code directive as JavaScript
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))
	parser.SetIncludedRanges([]Range{simpleRange(range1Start, range1End)})
	tree := parser.ParseWithOptions(chunkedInput(sourceCode, 3), nil, nil)
	assert.Equal(
		t,
		"(program "+
			"(expression_statement (call_expression function: (identifier) arguments: (arguments))))",
		tree.RootNode().ToSexp(),
	)

	// Parse both the first and third code directives as JavaScript, using the old tree as a
	// reference.
	parser.SetIncludedRanges([]Range{
		simpleRange(range1Start, range1End),
		simpleRange(range3Start, range3End),
	})
	tree2 := parser.ParseWithOptions(chunkedInput(sourceCode, 3), tree, nil)
	assert.Equal(
		t,
		"(program "+
			"(expression_statement (call_expression function: (identifier) arguments: (arguments))) "+
			"(expression_statement (call_expression function: (identifier) arguments: (arguments))))",
		tree2.RootNode().ToSexp(),
	)
	assert.Equal(
		t,
		[]Range{simpleRange(range1End, range3End)},
		tree2.ChangedRanges(tree),
	)

	// Parse all three code directives as JavaScript, using the old tree as a
	// reference.
	parser.SetIncludedRanges([]Range{
		simpleRange(range1Start, range1End),
		simpleRange(range2Start, range2End),
		simpleRange(range3Start, range3End),
	})
	tree3 := parser.Parse([]byte(sourceCode), tree2)
	assert.Equal(
		t,
		"(program "+
			"(expression_statement (call_expression function: (identifier) arguments: (arguments))) "+
			"(expression_statement (call_expression function: (identifier) arguments: (arguments))) "+
			"(expression_statement (call_expression function: (identifier) arguments: (arguments))))",
		tree3.RootNode().ToSexp(),
	)
	assert.Equal(
		t,
		[]Range{simpleRange(range2Start+1, range2End-1)},
		tree3.ChangedRanges(tree2),
	)
}

func TestParseStackRecursiveMergeErrorCostCalculationBug(t *testing.T) {
	sourceCode := []byte(`
fn main() {
  if n == 1 {
  } else if n == 2 {
  } else {
  }
}

let y = if x == 5 { 10 } else { 15 };

if foo && bar {}

if foo && bar || baz {}
`)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("rust"))

	tree := parser.Parse(sourceCode, nil)
	defer tree.Close()

	edit := &testEdit{
		position:      60,
		deletedLength: 63,
		insertedText:  []byte{},
	}
	performEdit(tree, &sourceCode, edit)

	parser.Parse(sourceCode, tree)
}

func TestParsingByHaltingAtOffset(t *testing.T) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("javascript"))

	sourceCode := "function foo() { return 1; }"
	sourceCode = strings.Repeat(sourceCode, 1000)

	seenByteOffsets := []uint32{}

	parser.ParseWithOptions(
		func(offset int, _ Point) []byte {
			if offset < len(sourceCode) {
				return []byte(sourceCode[offset:])
			}
			return []byte{}
		},
		nil,
		&ParseOptions{ProgressCallback: func(state ParseState) bool {
			seenByteOffsets = append(seenByteOffsets, state.CurrentByteOffset)
			return false
		}},
	)

	assert.Greater(t, len(seenByteOffsets), 100)
}

func TestPathologicalExample1(t *testing.T) {
	source := `*ss<s"ss<sqXqss<s._<s<sq<(qqX<sqss<s.ss<sqsssq<(qss<qssqXqss<s._<s<sq<(qqX<sqss<s.ss<sqsssq<(qss<sqss<sqss<s._<s<sq>(qqX<sqss<s.ss<sqsssq<(qss<sq&=ss<s<sqss<s._<s<sq<(qqX<sqss<s.ss<sqs`
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(getLanguage("cpp"))
	assert.NotNil(t, parser.Parse([]byte(source), nil))
}

func simpleRange(start, end int) Range {
	return Range{
		StartByte:  uint(start),
		EndByte:    uint(end),
		StartPoint: Point{0, uint(start)},
		EndPoint:   Point{0, uint(end)},
	}
}

func chunkedInput(text string, size int) func(int, Point) []byte {
	return func(offset int, _ Point) []byte {
		end := offset + size
		if end > len(text) {
			end = len(text)
		}
		return []byte(text[offset:end])
	}
}

type readRecorder struct {
	content      []byte
	indices_read []int
}

func newReadRecorder(content []byte) *readRecorder {
	return &readRecorder{
		content:      content,
		indices_read: []int{},
	}
}

func (r *readRecorder) Read(offset int) []byte {
	if offset < len(r.content) {
		i := sort.SearchInts(r.indices_read, offset)
		if i == len(r.indices_read) || r.indices_read[i] != offset {
			r.indices_read = append(r.indices_read, 0)
			copy(r.indices_read[i+1:], r.indices_read[i:])
			r.indices_read[i] = offset
		}
		return r.content[offset : offset+1]
	}
	return []byte{}
}

func (r *readRecorder) StringsRead() []string {
	var result []string
	var lastRange *struct{ start, end int }
	for _, index := range r.indices_read {
		if lastRange != nil {
			if lastRange.end == index {
				lastRange.end++
			} else {
				result = append(result, string(r.content[lastRange.start:lastRange.end]))
				lastRange = nil
			}
		} else {
			lastRange = &struct{ start, end int }{index, index + 1}
		}
	}
	if lastRange != nil {
		result = append(result, string(r.content[lastRange.start:lastRange.end]))
	}
	return result
}
