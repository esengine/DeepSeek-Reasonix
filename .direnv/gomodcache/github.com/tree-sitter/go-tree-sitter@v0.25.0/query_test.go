package tree_sitter_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	. "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func ExampleQueryCursor_Captures() {
	language := NewLanguage(tree_sitter_go.Language())
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	sourceCode := []byte(`
		package main

		import "fmt"

		func main() { fmt.Println("Hello, world!") }
	`)

	tree := parser.Parse(sourceCode, nil)
	defer tree.Close()

	query, err := NewQuery(
		language,
		`
		(function_declaration
			name: (identifier) @function.name
			body: (block) @function.block
		)
		`,
	)
	if err != nil {
		panic(err)
	}
	defer query.Close()

	qc := NewQueryCursor()
	defer qc.Close()

	captures := qc.Captures(query, tree.RootNode(), sourceCode)

	for match, index := captures.Next(); match != nil; match, index = captures.Next() {
		fmt.Printf(
			"Capture %d: %s\n",
			index,
			match.Captures[index].Node.Utf8Text(sourceCode),
		)
	}

	// Output:
	// Capture 0: main
	// Capture 1: { fmt.Println("Hello, world!") }
}

func ExampleQueryCursor_Matches() {
	language := NewLanguage(tree_sitter_go.Language())
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	sourceCode := []byte(`
		package main

		import "fmt"

		func main() { fmt.Println("Hello, world!") }
	`)

	tree := parser.Parse(sourceCode, nil)
	defer tree.Close()

	query, err := NewQuery(
		language,
		`
		(function_declaration
			name: (identifier) @function.name
			body: (block) @function.block
		)
		`,
	)
	if err != nil {
		panic(err)
	}
	defer query.Close()

	qc := NewQueryCursor()
	defer qc.Close()

	matches := qc.Matches(query, tree.RootNode(), sourceCode)

	for match := matches.Next(); match != nil; match = matches.Next() {
		for _, capture := range match.Captures {
			fmt.Printf(
				"Match %d, Capture %d (%s): %s\n",
				match.PatternIndex,
				capture.Index,
				query.CaptureNames()[capture.Index],
				capture.Node.Utf8Text(sourceCode),
			)
		}
	}

	// Output:
	// Match 0, Capture 0 (function.name): main
	// Match 0, Capture 1 (function.block): { fmt.Println("Hello, world!") }
}

func TestQueryErrorsOnInvalidSyntax(t *testing.T) {
	language := getLanguage("javascript")

	checkQuery := func(query *Query, err error) {
		defer query.Close()
		assert.NotNil(t, query)
		assert.Nil(t, err)
	}

	checkQueryErr := func(query *Query, err *QueryError, message string) {
		assert.Nil(t, query)
		assert.NotNil(t, err)
		assert.Equal(t, message, err.Message)
	}

	checkQuery(NewQuery(language, "(if_statement)"))
	checkQuery(NewQuery(language, "(if_statement condition:(parenthesized_expression (identifier)))"))

	// Mismatched parens
	query, err := NewQuery(language, "(if_statement")
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				"(if_statement",
				"             ^",
			},
			"\n",
		),
	)
	query, err = NewQuery(language, "; comment 1\n; comment 2\n  (if_statement))")
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				"  (if_statement))",
				"                ^",
			},
			"\n",
		),
	)

	// Return an error at the *beginning* of a bare identifier not followed a colon.
	// If there's a colon but no pattern, return an error at the end of the colon.
	query, err = NewQuery(language, "(if_statement identifier)")
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				"(if_statement identifier)",
				"              ^",
			},
			"\n",
		),
	)
	query, err = NewQuery(language, "(if_statement condition:)")
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				"(if_statement condition:)",
				"                        ^",
			},
			"\n",
		),
	)

	// Return an error at the beginning of an unterminated string.
	query, err = NewQuery(language, `(identifier) "h `)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`(identifier) "h `,
				`             ^`,
			},
			"\n",
		),
	)

	// Empty tree pattern
	query, err = NewQuery(language, `((identifier) ()`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`((identifier) ()`,
				`               ^`,
			},
			"\n",
		),
	)

	// Empty alternation
	query, err = NewQuery(language, `((identifier) [])`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`((identifier) [])`,
				`               ^`,
			},
			"\n",
		),
	)

	// Unclosed sibling expression with predicate
	query, err = NewQuery(language, `((identifier) (#a)`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`((identifier) (#a)`,
				`                  ^`,
			},
			"\n",
		),
	)

	// Unclosed predicate
	query, err = NewQuery(language, `((identifier) @x (#eq? @x a`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`((identifier) @x (#eq? @x a`,
				`                           ^`,
			},
			"\n",
		),
	)

	// Need at least one child node for a child anchor
	query, err = NewQuery(language, `(statement_block .)`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`(statement_block .)`,
				`                  ^`,
			},
			"\n",
		),
	)

	// Need a field name after a negated field operator
	query, err = NewQuery(language, `(statement_block ! (if_statement))`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`(statement_block ! (if_statement))`,
				`                   ^`,
			},
			"\n",
		),
	)

	// Unclosed alternation within a tree
	// tree-sitter/tree-sitter/issues/968
	query, err = NewQuery(getLanguage("c"), `(parameter_list [ ")" @foo)`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`(parameter_list [ ")" @foo)`,
				`                          ^`,
			},
			"\n",
		),
	)

	// Unclosed tree within an alternation
	// tree-sitter/tree-sitter/issues/1436
	query, err = NewQuery(getLanguage("python"), `[(unary_operator (_) @operand) (not_operator (_) @operand]`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`[(unary_operator (_) @operand) (not_operator (_) @operand]`,
				`                                                         ^`,
			},
			"\n",
		),
	)

	// MISSING keyword with full pattern
	query, err = NewQuery(getLanguage("c"), `(MISSING (function_declarator (identifier)))`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`(MISSING (function_declarator (identifier)))`,
				`         ^`,
			},
			"\n",
		),
	)

	// MISSING keyword with multiple identifiers
	query, err = NewQuery(getLanguage("c"), `(MISSING function_declarator function_declarator)`)
	checkQueryErr(
		query,
		err,
		strings.Join(
			[]string{
				`(MISSING function_declarator function_declarator)`,
				`                             ^`,
			},
			"\n",
		),
	)
}

func TestQueryErrorsOnInvalidSymbols(t *testing.T) {
	language := getLanguage("javascript")

	checkQueryErr := func(query *Query, err *QueryError, expected *QueryError) {
		assert.Nil(t, query)
		assert.NotNil(t, err)
		assert.Equal(t, expected, err)
	}

	query, err := NewQuery(language, "\">>>>\"")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  1,
			Column:  1,
			Kind:    QueryErrorNodeType,
			Message: ">>>>",
		},
	)

	query, err = NewQuery(language, "\"te\\\"st\"")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  1,
			Column:  1,
			Kind:    QueryErrorNodeType,
			Message: "te\\\"st",
		},
	)

	query, err = NewQuery(language, "\"\\\\\" @cap")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  1,
			Column:  1,
			Kind:    QueryErrorNodeType,
			Message: "\\\\",
		},
	)

	query, err = NewQuery(language, "(clas)")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  1,
			Column:  1,
			Kind:    QueryErrorNodeType,
			Message: "clas",
		},
	)

	query, err = NewQuery(language, "(if_statement (arrayyyyy))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  15,
			Column:  15,
			Kind:    QueryErrorNodeType,
			Message: "arrayyyyy",
		},
	)

	query, err = NewQuery(language, "(if_statement condition: (non_existent3))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  26,
			Column:  26,
			Kind:    QueryErrorNodeType,
			Message: "non_existent3",
		},
	)

	query, err = NewQuery(language, "(if_statement condit: (identifier))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  14,
			Column:  14,
			Kind:    QueryErrorField,
			Message: "condit",
		},
	)

	query, err = NewQuery(language, "(if_statement conditioning: (identifier))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  14,
			Column:  14,
			Kind:    QueryErrorField,
			Message: "conditioning",
		},
	)

	query, err = NewQuery(language, "(if_statement !alternativ)")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  15,
			Column:  15,
			Kind:    QueryErrorField,
			Message: "alternativ",
		},
	)

	query, err = NewQuery(language, "(if_statement !alternatives)")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  15,
			Column:  15,
			Kind:    QueryErrorField,
			Message: "alternatives",
		},
	)
}

func TestQueryErrorsOnInvalidPredicates(t *testing.T) {
	language := getLanguage("javascript")

	checkQueryErr := func(query *Query, err *QueryError, expected *QueryError) {
		assert.Nil(t, query)
		assert.NotNil(t, err)
		assert.Equal(t, expected, err)
	}

	query, err := NewQuery(language, "((identifier) @id (@id))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 19,
			Column: 19,
			Kind:   QueryErrorSyntax,
			Message: strings.Join([]string{
				"((identifier) @id (@id))",
				"                   ^",
			}, "\n"),
		},
	)

	query, err = NewQuery(language, "((identifier) @id (#eq? @id))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  0,
			Column:  0,
			Kind:    QueryErrorPredicate,
			Message: "Wrong number of arguments to #eq? predicate. Expected 2, got 1.",
		},
	)

	query, err = NewQuery(language, "((identifier) @id (#eq? @id @ok))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:     0,
			Offset:  29,
			Column:  29,
			Kind:    QueryErrorCapture,
			Message: "ok",
		},
	)
}

func TestQueryErrorsOnImpossiblePatterns(t *testing.T) {
	jsLang := getLanguage("javascript")
	rbLang := getLanguage("ruby")

	checkQuery := func(query *Query, err error) {
		defer query.Close()
		assert.NotNil(t, query)
		assert.Nil(t, err)
	}

	checkQueryErr := func(query *Query, err *QueryError, expected *QueryError) {
		assert.Nil(t, query)
		assert.NotNil(t, err)
		assert.Equal(t, expected, err)
	}

	query, err := NewQuery(jsLang, "(binary_expression left: (expression (identifier)) left: (expression (identifier)))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 51,
			Column: 51,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"(binary_expression left: (expression (identifier)) left: (expression (identifier)))",
					"                                                   ^",
				},
				"\n",
			),
		},
	)

	checkQuery(
		NewQuery(jsLang, "(function_declaration name: (identifier) (statement_block))"),
	)

	query, err = NewQuery(jsLang, "(function_declaration name: (statement_block))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 22,
			Column: 22,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"(function_declaration name: (statement_block))",
					"                      ^",
				},
				"\n",
			),
		},
	)

	checkQuery(NewQuery(rbLang, "(call receiver:(call))"))

	query, err = NewQuery(rbLang, "(call receiver:(binary))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 6,
			Column: 6,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"(call receiver:(binary))",
					"      ^",
				},
				"\n",
			),
		},
	)

	checkQuery(
		NewQuery(
			jsLang,
			`[
                    (function_expression (identifier))
                    (function_declaration (identifier))
                    (generator_function_declaration (identifier))
                ]`,
		),
	)

	query, err = NewQuery(
		jsLang,
		`[
                    (function_expression (identifier))
                    (function_declaration (object))
                    (generator_function_declaration (identifier))
            ]`,
	)
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    2,
			Offset: 99,
			Column: 42,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"                    (function_declaration (object))",
					"                                          ^",
				},
				"\n",
			),
		},
	)

	query, err = NewQuery(jsLang, "(identifier (identifier))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 12,
			Column: 12,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"(identifier (identifier))",
					"            ^",
				},
				"\n",
			),
		},
	)

	query, err = NewQuery(jsLang, "(true (true))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 6,
			Column: 6,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"(true (true))",
					"      ^",
				},
				"\n",
			),
		},
	)

	checkQuery(
		NewQuery(jsLang, "(if_statement condition: (parenthesized_expression (expression) @cond))"),
	)

	query, err = NewQuery(jsLang, "(if_statement condition: (expression))")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 14,
			Column: 14,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"(if_statement condition: (expression))",
					"              ^",
				},
				"\n",
			),
		},
	)

	query, err = NewQuery(jsLang, "(identifier/identifier)")
	checkQueryErr(
		query,
		err,
		&QueryError{
			Row:    0,
			Offset: 0,
			Column: 0,
			Kind:   QueryErrorStructure,
			Message: strings.Join(
				[]string{
					"(identifier/identifier)",
					"^",
				},
				"\n",
			),
		},
	)

	if jsLang.AbiVersion() >= 15 {
		query, err = NewQuery(jsLang, "(statement/identifier)")
		checkQueryErr(
			query,
			err,
			&QueryError{
				Row:    0,
				Offset: 0,
				Column: 0,
				Kind:   QueryErrorStructure,
				Message: strings.Join(
					[]string{
						"(statement/identifier)",
						"^",
					},
					"\n",
				),
			},
		)

		query, err = NewQuery(jsLang, "(statement/pattern)")
		checkQueryErr(
			query,
			err,
			&QueryError{
				Row:    0,
				Offset: 0,
				Column: 0,
				Kind:   QueryErrorStructure,
				Message: strings.Join(
					[]string{
						"(statement/pattern)",
						"^",
					},
					"\n",
				),
			},
		)
	}
}

func TestQueryVerifiesPossiblePatternsWithAliasedParentNodes(t *testing.T) {
	language := getLanguage("ruby")

	query, err := NewQuery(language, "(destructured_parameter (identifier))")
	assert.Nil(t, err)
	assert.NotNil(t, query)
	query.Close()

	query, err = NewQuery(language, "(destructured_parameter (string))")

	assert.Nil(t, query)
	assert.NotNil(t, err)
	assert.Equal(t, &QueryError{
		Kind:   QueryErrorStructure,
		Row:    0,
		Offset: 24,
		Column: 24,
		Message: strings.Join(
			[]string{
				"(destructured_parameter (string))",
				"                        ^",
			},
			"\n",
		),
	}, err)
}

func TestQueryMatchesWithSimplePattern(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "(function_declaration name: (identifier) @fn-name)")
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"function one() { two(); function three() {} }",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("fn-name", "one")),
			fmtMatch(0, fmtCapture("fn-name", "three")),
		},
	)
}

func TestQueryMatchesWithMultipleOnSameRoot(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`(class_declaration
			name: (identifier) @the-class-name
			(class_body
				(method_definition
					name: (property_identifier) @the-method-name)))`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		class Person {
			// the constructor
			constructor(name) { this.name = name; }

			// the getter
			getFullName() { return this.name; }
		}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("the-class-name", "Person"), fmtCapture("the-method-name", "constructor")),
			fmtMatch(0, fmtCapture("the-class-name", "Person"), fmtCapture("the-method-name", "getFullName")),
		},
	)
}

func TestQueryMatchesWithMultiplePatternsDifferentRoots(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(function_declaration name:(identifier) @fn-def)
		(call_expression function:(identifier) @fn-ref)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		function f1() {
			f2(f3());
		}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("fn-def", "f1")),
			fmtMatch(1, fmtCapture("fn-ref", "f2")),
			fmtMatch(1, fmtCapture("fn-ref", "f3")),
		},
	)
}

func TestQueryMatchesWithMultiplePatternsSameRoot(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(pair
			key: (property_identifier) @method-def
			value: (function_expression))

		(pair
			key: (property_identifier) @method-def
			value: (arrow_function))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a = {
			b: () => { return c; },
			d: function() { return d; }
		};
		`,
		[]formattedMatch{
			fmtMatch(1, fmtCapture("method-def", "b")),
			fmtMatch(0, fmtCapture("method-def", "d")),
		},
	)
}

func TestQueryMatchesWithNestingAndNoFields(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(array
			(array
				(identifier) @x1
				(identifier) @x2))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		[[a]];
		[[c, d], [e, f, g, h]];
		[[h], [i]];
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("x1", "c"), fmtCapture("x2", "d")),
			fmtMatch(0, fmtCapture("x1", "e"), fmtCapture("x2", "f")),
			fmtMatch(0, fmtCapture("x1", "e"), fmtCapture("x2", "g")),
			fmtMatch(0, fmtCapture("x1", "f"), fmtCapture("x2", "g")),
			fmtMatch(0, fmtCapture("x1", "e"), fmtCapture("x2", "h")),
			fmtMatch(0, fmtCapture("x1", "f"), fmtCapture("x2", "h")),
			fmtMatch(0, fmtCapture("x1", "g"), fmtCapture("x2", "h")),
		},
	)
}

func TestQueryMatchesWithManyResults(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "(array (identifier) @element)")
	defer query.Close()
	assert.Nil(t, err)

	source := strings.Repeat("[hello];\n", 50)
	expected := make([]formattedMatch, 50)
	for i := 0; i < 50; i++ {
		expected[i] = fmtMatch(0, fmtCapture("element", "hello"))
	}

	assertQueryMatches(t, language, query, source, expected)
}

func TestQueryMatchesWithManyOverlappingResults(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(call_expression
			function: (member_expression
				property: (property_identifier) @method))
		(call_expression
			function: (identifier) @function)
		((identifier) @constant
			(#match? @constant "[A-Z\\d_]+"))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	count := 1024

	// Deeply nested chained function calls:
	// a
	//    .foo(bar(BAZ))
	//    .foo(bar(BAZ))
	//    .foo(bar(BAZ))
	//    ...
	source := "a"
	source += strings.Repeat("\n  .foo(bar(BAZ))", count)

	expected := make([]formattedMatch, 3*count)
	for i := 0; i < 3*count; i += 3 {
		expected[i] = fmtMatch(uint(i%3), fmtCapture("method", "foo"))
		expected[i+1] = fmtMatch(uint(i%3+1), fmtCapture("function", "bar"))
		expected[i+2] = fmtMatch(uint(i%3+2), fmtCapture("constant", "BAZ"))
	}

	assertQueryMatches(t, language, query, source, expected)
}

func TestQueryMatchesCapturingErrorNodes(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(ERROR (identifier) @the-error-identifier) @the-error
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"function a(b,, c, d :e:) {}",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("the-error", ":e:"), fmtCapture("the-error-identifier", "e")),
		},
	)
}

func TestQueryMatchesCapturingMissingNodes(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(MISSING
			; Comments should be valid
		) @missing
		(MISSING
			; Comments should be valid
			";"
			; Comments should be valid
		) @missing-semicolon
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
        x = function(a) { b; } function(c) { d; }
        //                    ^ MISSING semicolon here
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("missing", "")),
			fmtMatch(1, fmtCapture("missing-semicolon", "")),
		},
	)

	language = getLanguage("c")
	query, err = NewQuery(
		language,
		`
		(MISSING field_identifier) @missing-field-ident
		(MISSING identifier) @missing-ident
		(MISSING) @missing-anything
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		int main() {
			if (a.) {
			//    ^ MISSING field_identifier here
			b();
			c();

			if (*) d();
			//   ^ MISSING identifier here
			}
		}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("missing-field-ident", "")),
			fmtMatch(2, fmtCapture("missing-anything", "")),
			fmtMatch(1, fmtCapture("missing-ident", "")),
			fmtMatch(2, fmtCapture("missing-anything", "")),
		},
	)
}

func TestQueryMatchesWithExtraChildren(t *testing.T) {
	language := getLanguage("ruby")
	query, err := NewQuery(
		language,
		`
		(program(comment) @top_level_comment)
		(argument_list (heredoc_body) @heredoc_in_args)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
            # top-level
            puts(
                # not-top-level
                <<-IN_ARGS, bar.baz
                HELLO
                IN_ARGS
            )

            puts <<-NOT_IN_ARGS
            NO
            NOT_IN_ARGS
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("top_level_comment", "# top-level")),
			fmtMatch(1, fmtCapture("heredoc_in_args", "\n                HELLO\n                IN_ARGS")),
		},
	)
}

func TestQueryMatchesWithNamedWildcard(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(return_statement (_) @the-return-value)
		(binary_expression operator: _ @the-operator)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"return a + b - c;",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("the-return-value", "a + b - c")),
			fmtMatch(1, fmtCapture("the-operator", "+")),
			fmtMatch(1, fmtCapture("the-operator", "-")),
		},
	)
}

func TestQueryMatchesWithWildcardAtTheRoot(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(_
			(comment) @doc
			.
			(function_declaration
				name: (identifier) @name))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"/* one */ var x; /* two */ function y() {} /* three */ class Z {}",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("doc", "/* two */"), fmtCapture("name", "y")),
		},
	)

	query, err = NewQuery(
		language,
		`
		(_ (string) @a)
		(_ (number) @b)
		(_ (true) @c)
		(_ (false) @d)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"['hi', x(true), {y: false}]",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("a", "'hi'")),
			fmtMatch(2, fmtCapture("c", "true")),
			fmtMatch(3, fmtCapture("d", "false")),
		},
	)
}

func TestQueryMatchesWithWildcardWithinWildcard(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(_ (_) @child) @parent
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"/* a */ b; c;",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("parent", "/* a */ b; c;"), fmtCapture("child", "/* a */")),
			fmtMatch(0, fmtCapture("parent", "/* a */ b; c;"), fmtCapture("child", "b;")),
			fmtMatch(0, fmtCapture("parent", "b;"), fmtCapture("child", "b")),
			fmtMatch(0, fmtCapture("parent", "/* a */ b; c;"), fmtCapture("child", "c;")),
			fmtMatch(0, fmtCapture("parent", "c;"), fmtCapture("child", "c")),
		},
	)
}

func TestQueryMatchesWithImmediateSiblings(t *testing.T) {
	language := getLanguage("python")

	// The immediate child operator '.' can be used in three similar ways:
	// 1. Before the first child node in a pattern, it means that there cannot be any named
	//    siblings before that child node.
	// 2. After the last child node in a pattern, it means that there cannot be any named
	//    sibling after that child node.
	// 2. Between two child nodes in a pattern, it specifies that there cannot be any named
	//    siblings between those two child snodes.
	query, err := NewQuery(
		language,
		`
            (dotted_name
                (identifier) @parent
                .
                (identifier) @child)
            (dotted_name
                (identifier) @last-child
                .)
            (list
                .
                (_) @first-element)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"import a.b.c.d; return [w, [1, y], z]",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("parent", "a"), fmtCapture("child", "b")),
			fmtMatch(0, fmtCapture("parent", "b"), fmtCapture("child", "c")),
			fmtMatch(0, fmtCapture("parent", "c"), fmtCapture("child", "d")),
			fmtMatch(1, fmtCapture("last-child", "d")),
			fmtMatch(2, fmtCapture("first-element", "w")),
			fmtMatch(2, fmtCapture("first-element", "1")),
		},
	)

	query, err = NewQuery(
		language,
		`
			(block . (_) @first-stmt)
			(block (_) @stmt)
			(block (_) @last-stmt .)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
            if a:
                b()
                c()
                if d(): e(); f()
                g()
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("first-stmt", "b()")),
			fmtMatch(1, fmtCapture("stmt", "b()")),
			fmtMatch(1, fmtCapture("stmt", "c()")),
			fmtMatch(1, fmtCapture("stmt", "if d(): e(); f()")),
			fmtMatch(0, fmtCapture("first-stmt", "e()")),
			fmtMatch(1, fmtCapture("stmt", "e()")),
			fmtMatch(1, fmtCapture("stmt", "f()")),
			fmtMatch(2, fmtCapture("last-stmt", "f()")),
			fmtMatch(1, fmtCapture("stmt", "g()")),
			fmtMatch(2, fmtCapture("last-stmt", "g()")),
		},
	)
}

func TestQueryMatchesWithLastNamedChild(t *testing.T) {
	language := getLanguage("c")
	query, err := NewQuery(
		language,
		`
		(compound_statement
			(_)
			(_)
			(expression_statement
				(identifier) @last_id) .)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		void one() { a; b; c; }
		void two() { d; e; }
		void three() { f; g; h; i; }
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("last_id", "c")),
			fmtMatch(0, fmtCapture("last_id", "i")),
		},
	)
}

func TestQueryMatchesWithNegatedFields(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(import_specifier
			!alias
			name: (identifier) @import_name)

		(export_specifier
			!alias
			name: (identifier) @export_name)

		(export_statement
			!decorator
			!source
			(_) @exported)

		; This negated field list is an extension of a previous
		; negated field list. The order of the children and negated
		; fields don't matter.
		(export_statement
			!decorator
			!source
			(_) @exported_expr
			!declaration)

		; This negated field list is a prefix of a previous
		; negated field list.
		(export_statement
			!decorator
			(_) @export_child .)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		import {a as b, c} from 'p1';
		export {g, h as i} from 'p2';

		@foo
		export default 1;

		export var j = 1;

		export default k;
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("import_name", "c")),
			fmtMatch(1, fmtCapture("export_name", "g")),
			fmtMatch(4, fmtCapture("export_child", "'p2'")),
			fmtMatch(2, fmtCapture("exported", "var j = 1;")),
			fmtMatch(4, fmtCapture("export_child", "var j = 1;")),
			fmtMatch(2, fmtCapture("exported", "k")),
			fmtMatch(3, fmtCapture("exported_expr", "k")),
			fmtMatch(4, fmtCapture("export_child", "k")),
		},
	)
}

func TestQueryMatchesWithFieldAtRoot(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "name: (identifier) @name")
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a();
		function b() {}
		class c extends d {}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("name", "b")),
			fmtMatch(0, fmtCapture("name", "c")),
		},
	)
}

func TestQueryMatchesWithRepeatedLeafNodes(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(
			(comment)+ @doc
			.
			(class_declaration
				name: (identifier) @name)
		)

		(
			(comment)+ @doc
			.
			(function_declaration
				name: (identifier) @name)
		)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		// one
		// two
		a();

		// three
		{
			// four
			// five
			// six
			class B {}

			// seven
			c();

			// eight
			function d() {}
		}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("doc", "// four"), fmtCapture("doc", "// five"), fmtCapture("doc", "// six"), fmtCapture("name", "B")),
			fmtMatch(1, fmtCapture("doc", "// eight"), fmtCapture("name", "d")),
		},
	)
}

func TestQueryMatchesWithOptionalNodesInsideOfRepetitions(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, `(array (","? (number) @num)+)`)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"var a = [1, 2, 3, 4]",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("num", "1"), fmtCapture("num", "2"), fmtCapture("num", "3"), fmtCapture("num", "4")),
		},
	)
}

func TestQueryMatchesWithTopLevelRepetitions(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, `(comment)+ @doc`)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		// a
		// b
		// c

		d()

		// e
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("doc", "// a"), fmtCapture("doc", "// b"), fmtCapture("doc", "// c")),
			fmtMatch(0, fmtCapture("doc", "// e")),
		},
	)
}

func TestQueryMatchesWithNonTerminalRepetitionsWithinRoot(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "(_ (expression_statement (identifier) @id)+)")
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		function f() {
			d;
			e;
			f;
			g;
		}
		a;
		b;
		c;
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("id", "d"), fmtCapture("id", "e"), fmtCapture("id", "f"), fmtCapture("id", "g")),
			fmtMatch(0, fmtCapture("id", "a"), fmtCapture("id", "b"), fmtCapture("id", "c")),
		},
	)
}

func TestQueryMatchesWithNestedRepetitions(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, `((variable_declaration (","? (variable_declarator name: (identifier) @x))+)+)`)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		var a = b, c, d
		var e, f

		// more
		var g
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("x", "a"), fmtCapture("x", "c"), fmtCapture("x", "d"), fmtCapture("x", "e"), fmtCapture("x", "f")),
			fmtMatch(0, fmtCapture("x", "g")),
		},
	)
}

func TestQueryMatchesWithMultipleRepetitionPatternsThatIntersectOtherPattern(t *testing.T) {
	language := getLanguage("javascript")

	query, err := NewQuery(
		language,
		`
		(call_expression
			function: (member_expression
				property: (property_identifier) @name)) @ref.method

		((comment)* @doc (function_declaration))
		((comment)* @doc (generator_function_declaration))
		((comment)* @doc (class_declaration))
		((comment)* @doc (lexical_declaration))
		((comment)* @doc (variable_declaration))
		((comment)* @doc (method_definition))

		(comment) @comment
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	// Here, a series of comments occurs in the middle of a match of the first
	// pattern. To avoid exceeding the storage limits and discarding that outer
	// match, the comment-related matches need to be managed efficiently.
	source := fmt.Sprintf("theObject\n%s\n.theMethod()", strings.Repeat("  // the comment\n", 64))

	expected := make([]formattedMatch, 65)
	for i := 0; i < 65; i++ {
		expected[i] = fmtMatch(7, fmtCapture("comment", "// the comment"))
	}
	expected[64] = fmtMatch(0, fmtCapture("ref.method", source), fmtCapture("name", "theMethod"))

	assertQueryMatches(t, language, query, source, expected)
}

func TestQueryMatchesWithTrailingRepetitionsOfLastChild(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, `(unary_expression (primary_expression)+ @operand)`)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a = typeof (!b && ~c);
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("operand", "b")),
			fmtMatch(0, fmtCapture("operand", "c")),
			fmtMatch(0, fmtCapture("operand", "(!b && ~c)")),
		},
	)
}

func TestQueryMatchesWithLeadingZeroOrMoreRepeatedLeafNodes(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(
			(comment)* @doc
			.
			(function_declaration
				name: (identifier) @name)
		)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		function a() {
			// one
			var b;

			function c() {}

			// two
			// three
			var d;

			// four
			// five
			function e() {

			}
		}

		// six
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("name", "a")),
			fmtMatch(0, fmtCapture("name", "c")),
			fmtMatch(0, fmtCapture("doc", "// four"), fmtCapture("doc", "// five"), fmtCapture("name", "e")),
		},
	)
}

func TestQueryMatchesWithTrailingOptionalNodes(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(class_declaration
			name: (identifier) @class
			(class_heritage
				(identifier) @superclass)?)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		class A {}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("class", "A")),
		},
	)

	assertQueryMatches(
		t,
		language,
		query,
		`
		class A {}
		class B extends C {}
		class D extends (E.F) {}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("class", "A")),
			fmtMatch(0, fmtCapture("class", "B"), fmtCapture("superclass", "C")),
			fmtMatch(0, fmtCapture("class", "D")),
		},
	)
}

func TestQueryMatchesWithNestedOptionalNodes(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(call_expression
			function: (identifier) @outer-fn
			arguments: (arguments
				(call_expression
					function: (identifier) @inner-fn
					arguments: (arguments
						(number)? @num))?))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a(b, c(), d(null, 1, 2))
		e()
		f(g())
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("outer-fn", "a"), fmtCapture("inner-fn", "c")),
			fmtMatch(0, fmtCapture("outer-fn", "c")),
			fmtMatch(0, fmtCapture("outer-fn", "a"), fmtCapture("inner-fn", "d"), fmtCapture("num", "1")),
			fmtMatch(0, fmtCapture("outer-fn", "a"), fmtCapture("inner-fn", "d"), fmtCapture("num", "2")),
			fmtMatch(0, fmtCapture("outer-fn", "d")),
			fmtMatch(0, fmtCapture("outer-fn", "e")),
			fmtMatch(0, fmtCapture("outer-fn", "f"), fmtCapture("inner-fn", "g")),
			fmtMatch(0, fmtCapture("outer-fn", "g")),
		},
	)
}

func TestQueryMatchesWithRepeatedInternalNodes(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(_
			(method_definition
				(decorator (identifier) @deco)+
				name: (property_identifier) @name))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		class A {
			@c
			@d
			e() {}
		}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("deco", "c"), fmtCapture("deco", "d"), fmtCapture("name", "e")),
		},
	)
}

func TestQueryMatchesWithSimpleAlternatives(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(pair
			key: [(property_identifier) (string)] @key
			value: [(function_expression) @val1 (arrow_function) @val2])
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a = {
			b: c,
			'd': e => f,
			g: {
				h: function i() {},
				'x': null,
				j: _ => k
			},
			'l': function m() {},
		};
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("key", "'d'"), fmtCapture("val2", "e => f")),
			fmtMatch(0, fmtCapture("key", "h"), fmtCapture("val1", "function i() {}")),
			fmtMatch(0, fmtCapture("key", "j"), fmtCapture("val2", "_ => k")),
			fmtMatch(0, fmtCapture("key", "'l'"), fmtCapture("val1", "function m() {}")),
		},
	)
}

func TestQueryMatchesWithAlternativesInRepetitions(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(array
			[(identifier) (string)] @el
			.
			(
				","
				.
				[(identifier) (string)] @el
			)*)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a = [b, 'c', d, 1, e, 'f', 'g', h];
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("el", "b"), fmtCapture("el", "'c'"), fmtCapture("el", "d")),
			fmtMatch(0, fmtCapture("el", "e"), fmtCapture("el", "'f'"), fmtCapture("el", "'g'"), fmtCapture("el", "h")),
		},
	)
}

func TestQueryMatchesWithAlternativesAtRoot(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		[
			"if"
			"else"
			"function"
			"throw"
			"return"
		] @keyword
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		function a(b, c, d) {
			if (b) {
				return c;
			} else {
				throw d;
			}
		}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("keyword", "function")),
			fmtMatch(0, fmtCapture("keyword", "if")),
			fmtMatch(0, fmtCapture("keyword", "return")),
			fmtMatch(0, fmtCapture("keyword", "else")),
			fmtMatch(0, fmtCapture("keyword", "throw")),
		},
	)
}

func TestQueryMatchesWithAlternativesUnderFields(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(assignment_expression
			left: [
				(identifier) @variable
				(member_expression property: (property_identifier) @variable)
			])
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a = b;
		b = c.d;
		e.f = g;
		h.i = j.k;
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("variable", "a")),
			fmtMatch(0, fmtCapture("variable", "b")),
			fmtMatch(0, fmtCapture("variable", "f")),
			fmtMatch(0, fmtCapture("variable", "i")),
		},
	)
}

func TestQueryMatchesInLanguageWithSimpleAliases(t *testing.T) {
	language := getLanguage("html")

	// HTML uses different tokens to track start tags names, end
	// tag names, script tag names, and style tag names. All of
	// these tokens are aliased to `tag_name`.
	query, err := NewQuery(language, "(tag_name) @tag")
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		<div>
			<script>hi</script>
			<style>hi</style>
		</div>
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("tag", "div")),
			fmtMatch(0, fmtCapture("tag", "script")),
			fmtMatch(0, fmtCapture("tag", "script")),
			fmtMatch(0, fmtCapture("tag", "style")),
			fmtMatch(0, fmtCapture("tag", "style")),
			fmtMatch(0, fmtCapture("tag", "div")),
		},
	)
}

func TestQueryMatchesWithDifferentTokensWithTheSameStringValue(t *testing.T) {
	language := getLanguage("rust")
	query, err := NewQuery(
		language,
		`
		"<" @less
		">" @greater
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		"const A: B<C> = d < e || f > g;",
		[]formattedMatch{
			fmtMatch(0, fmtCapture("less", "<")),
			fmtMatch(1, fmtCapture("greater", ">")),
			fmtMatch(0, fmtCapture("less", "<")),
			fmtMatch(1, fmtCapture("greater", ">")),
		},
	)
}

func TestQueryMatchesWithTooManyPermutationsToTrack(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(array (identifier) @pre (identifier) @post)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := strings.Repeat("hello, ", 50)
	source = "[" + source + "];"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()
	cursor.SetMatchLimit(32)

	// For this pathological query, some match permutations will be dropped.
	// Just check that a subset of the results are returned, and crash or
	// leak occurs.
	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source)[0], fmtMatch(0, fmtCapture("pre", "hello"), fmtCapture("post", "hello")))
	assert.True(t, cursor.DidExceedMatchLimit())
}

func TestQuerySiblingPatternsDontMatchChildrenOfAnError(t *testing.T) {
	language := getLanguage("rust")
	query, err := NewQuery(
		language,
		`
		("{" @open "}" @close)

		[
			(line_comment)
			(block_comment)
		] @comment

		("<" @first "<" @second)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	// Most of the document will fail to parse, resulting in a
	// large number of tokens that are *direct* children of an
	// ERROR node.
	//
	// These children should still match, unless they are part
	// of a "non-rooted" pattern, in which there are multiple
	// top-level sibling nodes. Those patterns should not match
	// directly inside of an error node, because the contents of
	// an error node are not syntactically well-structured, so we
	// would get many spurious matches.
	source := `
            fn a() {}

            <<<<<<<<<< add pub b fn () {}
            // comment 1
            pub fn b() {
            /* comment 2 */
            ==========
            pub fn c() {
            // comment 3
            >>>>>>>>>> add pub c fn () {}
            }
	`

	assertQueryMatches(
		t,
		language,
		query,
		source,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("open", "{"), fmtCapture("close", "}")),
			fmtMatch(1, fmtCapture("comment", "// comment 1")),
			fmtMatch(1, fmtCapture("comment", "/* comment 2 */")),
			fmtMatch(1, fmtCapture("comment", "// comment 3")),
		},
	)
}

func TestQueryMatchesWithAlternativesAndTooManyPermutationToTrack(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(
			(comment) @doc
			; not immediate
			(class_declaration) @class
		)

		(call_expression
			function: [
				(identifier) @function
				(member_expression property: (property_identifier) @method)
			])
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := strings.Repeat("/* hi */ a.b(); ", 50)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()
	cursor.SetMatchLimit(32)

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	expected := make([]formattedMatch, 50)
	for i := 0; i < 50; i++ {
		expected[i] = fmtMatch(1, fmtCapture("method", "b"))
	}
	assert.Equal(t, expected, collectMatches(matches, query, source))
}

func TestRepetitionsBeforeWithAlternatives(t *testing.T) {
	language := getLanguage("rust")
	query, err := NewQuery(
		language,
		`
		(
			(line_comment)* @comment
			.
			[
				(struct_item name: (_) @name)
				(function_item name: (_) @name)
				(enum_item name: (_) @name)
				(impl_item type: (_) @name)
			]
		)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		// a
		// b
		fn c() {}

		// d
		// e
		impl F {}
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("comment", "// a"), fmtCapture("comment", "// b"), fmtCapture("name", "c")),
			fmtMatch(0, fmtCapture("comment", "// d"), fmtCapture("comment", "// e"), fmtCapture("name", "F")),
		},
	)
}

func TestQueryMatchesWithAnonymousTokens(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		";" @punctuation
		"&&" @operator
		"\"" @quote
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`foo(a && "b");`,
		[]formattedMatch{
			fmtMatch(1, fmtCapture("operator", "&&")),
			fmtMatch(2, fmtCapture("quote", "\"")),
			fmtMatch(2, fmtCapture("quote", "\"")),
			fmtMatch(0, fmtCapture("punctuation", ";")),
		},
	)
}

func TestQueryMatchesWithSupertypes(t *testing.T) {
	language := getLanguage("python")
	query, err := NewQuery(
		language,
		`
		(argument_list (expression) @arg)

		(keyword_argument
			value: (expression) @kw_arg)

		(assignment
			left: (identifier) @var_def)

		(primary_expression/identifier) @var_ref
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a = b.c(
			[d],
			# a comment
			e=f
		)
		`,
		[]formattedMatch{
			fmtMatch(2, fmtCapture("var_def", "a")),
			fmtMatch(3, fmtCapture("var_ref", "b")),
			fmtMatch(0, fmtCapture("arg", "[d]")),
			fmtMatch(3, fmtCapture("var_ref", "d")),
			fmtMatch(1, fmtCapture("kw_arg", "f")),
			fmtMatch(3, fmtCapture("var_ref", "f")),
		},
	)
}

func TestQueryMatchesWithinByteRange(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "(identifier) @element")
	defer query.Close()
	assert.Nil(t, err)

	source := "[a, b, c, d, e, f, g]"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.SetByteRange(0, 8).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(0, fmtCapture("element", "a")),
		fmtMatch(0, fmtCapture("element", "b")),
		fmtMatch(0, fmtCapture("element", "c")),
	})

	matches = cursor.SetByteRange(5, 15).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(0, fmtCapture("element", "c")),
		fmtMatch(0, fmtCapture("element", "d")),
		fmtMatch(0, fmtCapture("element", "e")),
	})

	// An end byte of zero indicates there is no end
	matches = cursor.SetByteRange(12, 0).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(0, fmtCapture("element", "e")),
		fmtMatch(0, fmtCapture("element", "f")),
		fmtMatch(0, fmtCapture("element", "g")),
	})
}

func TestQueryMatchesWithinPointRange(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "(identifier) @element")
	defer query.Close()
	assert.Nil(t, err)

	source := `[
  a, b,
  c, d,
  e, f,
  g, h,
  i, j,
  k, l,
]`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.SetPointRange(NewPoint(1, 0), NewPoint(2, 3)).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(0, fmtCapture("element", "a")),
		fmtMatch(0, fmtCapture("element", "b")),
		fmtMatch(0, fmtCapture("element", "c")),
	})

	matches = cursor.SetPointRange(NewPoint(2, 0), NewPoint(3, 3)).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(0, fmtCapture("element", "c")),
		fmtMatch(0, fmtCapture("element", "d")),
		fmtMatch(0, fmtCapture("element", "e")),
	})

	// Zero end point is treated like no end point.
	matches = cursor.SetPointRange(NewPoint(4, 1), NewPoint(0, 0)).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(0, fmtCapture("element", "g")),
		fmtMatch(0, fmtCapture("element", "h")),
		fmtMatch(0, fmtCapture("element", "i")),
		fmtMatch(0, fmtCapture("element", "j")),
		fmtMatch(0, fmtCapture("element", "k")),
		fmtMatch(0, fmtCapture("element", "l")),
	})
}

func TestQueryCapturesWithinByteRange(t *testing.T) {
	language := getLanguage("c")
	query, err := NewQuery(
		language,
		`
		(call_expression
			function: (identifier) @function
			arguments: (argument_list (string_literal) @string.arg))

		(string_literal) @string
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := `DEFUN ("safe-length", Fsafe_length, Ssafe_length, 1, 1, 0)`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.SetByteRange(3, 27).Captures(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		collectCaptures(captures, query, source),
		[]formattedCapture{fmtCapture("function", "DEFUN"), fmtCapture("string.arg", "\"safe-length\""), fmtCapture("string", "\"safe-length\"")},
	)
}

func TestQueryCursorNextCaptureWithByteRange(t *testing.T) {
	language := getLanguage("python")
	query, err := NewQuery(
		language,
		`
		(function_definition name: (identifier) @function)
		(attribute attribute: (identifier) @property)
		((identifier) @variable)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := "def func():\n  foo.bar.baz()\n"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.SetByteRange(12, 17).Captures(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		collectCaptures(captures, query, source),
		[]formattedCapture{fmtCapture("variable", "foo")},
	)
}

func TestQueryCursorNextCaptureWithPointRange(t *testing.T) {
	language := getLanguage("python")
	query, err := NewQuery(
		language,
		`
		(function_definition name: (identifier) @function)
		(attribute attribute: (identifier) @property)
		((identifier) @variable)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	var source string = "def func():\n  foo.bar.baz()\n"
	//                   ^            ^    ^          ^
	// byte_pos          0           12    17        27
	// point_pos         (0,0)      (1,0)  (1,5)    (1,15)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.SetPointRange(NewPoint(1, 0), NewPoint(1, 5)).Captures(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		collectCaptures(captures, query, source),
		[]formattedCapture{fmtCapture("variable", "foo")},
	)
}

func TestQueryMatchesWithUnrootedPatternsIntersectingByteRange(t *testing.T) {
	language := getLanguage("rust")
	query, err := NewQuery(
		language,
		`
		("{" @left "}" @right)
		("<" @left ">" @right)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := "mod a { fn a<B: C, D: E>(f: B) { g(f) } }"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	// within the type parameter list
	offset := uint(strings.Index(source, "D: E>"))
	matches := cursor.SetByteRange(offset, offset).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(1, fmtCapture("left", "<"), fmtCapture("right", ">")),
		fmtMatch(0, fmtCapture("left", "{"), fmtCapture("right", "}")),
	})

	// from within the type parameter list to within the function body
	startOffset := uint(strings.Index(source, "D: E>"))
	endOffset := uint(strings.Index(source, "g(f)"))
	matches = cursor.SetByteRange(startOffset, endOffset).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, collectMatches(matches, query, source), []formattedMatch{
		fmtMatch(1, fmtCapture("left", "<"), fmtCapture("right", ">")),
		fmtMatch(0, fmtCapture("left", "{"), fmtCapture("right", "}")),
		fmtMatch(0, fmtCapture("left", "{"), fmtCapture("right", "}")),
	})
}

func TestQueryMatchesWithWildcardAtRootIntersectingByteRange(t *testing.T) {
	language := getLanguage("python")
	query, err := NewQuery(
		language,
		`
		[
			(_ body: (block))
			(_ consequence: (block))
		] @indent
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := strings.TrimSpace(`
            class A:
                def b():
                    if c:
                        d
                    else:
                        e
		`)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	// After the first line of the class definition
	offset := uint(strings.Index(source, "A:")) + 2
	matches := cursor.SetByteRange(offset, offset).Matches(query, tree.RootNode(), []byte(source))
	kinds := make([]string, 0)
	for match := matches.Next(); match != nil; match = matches.Next() {
		kinds = append(kinds, match.Captures[0].Node.Kind())
	}
	assert.Equal(t, []string{"class_definition"}, kinds)

	// After the first line of the function definition
	offset = uint(strings.Index(source, "b():")) + 4
	matches = cursor.SetByteRange(offset, offset).Matches(query, tree.RootNode(), []byte(source))
	kinds = make([]string, 0)
	for match := matches.Next(); match != nil; match = matches.Next() {
		kinds = append(kinds, match.Captures[0].Node.Kind())
	}
	assert.Equal(t, []string{"class_definition", "function_definition"}, kinds)

	// After the first line of the if statement
	offset = uint(strings.Index(source, "c:")) + 2
	matches = cursor.SetByteRange(offset, offset).Matches(query, tree.RootNode(), []byte(source))
	kinds = make([]string, 0)
	for match := matches.Next(); match != nil; match = matches.Next() {
		kinds = append(kinds, match.Captures[0].Node.Kind())
	}
	assert.Equal(t, []string{"class_definition", "function_definition", "if_statement"}, kinds)
}

func TestQueryCapturesWithinByteRangeAssignedAfterIterating(t *testing.T) {
	language := getLanguage("rust")
	query, err := NewQuery(
		language,
		`
		(function_item
			name: (identifier) @fn_name)

		(mod_item
			name: (identifier) @mod_name
			body: (declaration_list
				"{" @lbrace
				"}" @rbrace))

		; functions that return Result<()>
		((function_item
			return_type: (generic_type
				type: (type_identifier) @result
				type_arguments: (type_arguments
					(unit_type)))
			body: _ @fallible_fn_body)
			(#eq? @result "Result"))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := `
        mod m1 {
            mod m2 {
                fn f1() -> Option<()> { Some(()) }
            }
            fn f2() -> Result<()> { Ok(()) }
            fn f3() {}
        }
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))

	// Retrieve some captures
	results := make([]formattedCapture, 0)
	for i := 0; i < 5; i++ {
		match, captureIndex := captures.Next()
		if match == nil {
			break
		}
		capture := match.Captures[captureIndex]
		results = append(results, fmtCapture(query.CaptureNames()[capture.Index], source[capture.Node.StartByte():capture.Node.EndByte()]))
	}
	assert.Equal(
		t,
		[]formattedCapture{
			fmtCapture("mod_name", "m1"),
			fmtCapture("lbrace", "{"),
			fmtCapture("mod_name", "m2"),
			fmtCapture("lbrace", "{"),
			fmtCapture("fn_name", "f1"),
		},
		results,
	)

	// Advance to a range that only partially intersects some matches.
	// Captures from these matches are reported, but only those that
	// intersect the range.
	results = make([]formattedCapture, 0)
	captures.SetByteRange(uint(strings.Index(source, "Ok")), uint(len(source)))
	for match, captureIndex := captures.Next(); match != nil; match, captureIndex = captures.Next() {
		capture := match.Captures[captureIndex]
		results = append(results, fmtCapture(query.CaptureNames()[capture.Index], source[capture.Node.StartByte():capture.Node.EndByte()]))
	}
	assert.Equal(
		t,
		[]formattedCapture{
			fmtCapture("fallible_fn_body", "{ Ok(()) }"),
			fmtCapture("fn_name", "f3"),
			fmtCapture("rbrace", "}"),
		},
		results,
	)
}

func TestQueryMatchesWithinRangeOfLongRepetition(t *testing.T) {
	language := getLanguage("rust")
	query, err := NewQuery(
		language,
		`
		(function_item name: (identifier) @fn-name)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := strings.TrimSpace(`
            fn zero() {}
            fn one() {}
            fn two() {}
            fn three() {}
            fn four() {}
            fn five() {}
            fn six() {}
            fn seven() {}
            fn eight() {}
            fn nine() {}
            fn ten() {}
            fn eleven() {}
            fn twelve() {}
	`)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.SetPointRange(NewPoint(8, 0), NewPoint(20, 0)).Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("fn-name", "eight")),
			fmtMatch(0, fmtCapture("fn-name", "nine")),
			fmtMatch(0, fmtCapture("fn-name", "ten")),
			fmtMatch(0, fmtCapture("fn-name", "eleven")),
			fmtMatch(0, fmtCapture("fn-name", "twelve")),
		},
		collectMatches(matches, query, source),
	)
}

func TestQueryMatchesDifferentQueriesSameCursor(t *testing.T) {
	language := getLanguage("javascript")
	query1, err := NewQuery(
		language,
		`
		(array (identifier) @id1)
		`,
	)
	defer query1.Close()
	assert.Nil(t, err)

	query2, err := NewQuery(
		language,
		`
		(array (identifier) @id1)
		(pair (identifier) @id2)
		`,
	)
	defer query2.Close()
	assert.Nil(t, err)

	query3, err := NewQuery(
		language,
		`
		(array (identifier) @id1)
		(pair (identifier) @id2)
		(parenthesized_expression (identifier) @id3)
		`,
	)
	defer query3.Close()
	assert.Nil(t, err)

	source := "[a, {b: b}, (c)];"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query1, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("id1", "a")),
		},
		collectMatches(matches, query1, source),
	)

	matches = cursor.Matches(query3, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("id1", "a")),
			fmtMatch(1, fmtCapture("id2", "b")),
			fmtMatch(2, fmtCapture("id3", "c")),
		},
		collectMatches(matches, query3, source),
	)

	matches = cursor.Matches(query2, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("id1", "a")),
			fmtMatch(1, fmtCapture("id2", "b")),
		},
		collectMatches(matches, query2, source),
	)
}

func TestQueryMatchesWithMultipleCapturesOnANode(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(function_declaration
			(identifier) @name1 @name2 @name3
			(statement_block) @body1 @body2)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := "function foo() { return 1; }"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(0,
				fmtCapture("name1", "foo"),
				fmtCapture("name2", "foo"),
				fmtCapture("name3", "foo"),
				fmtCapture("body1", "{ return 1; }"),
				fmtCapture("body2", "{ return 1; }"),
			),
		},
		collectMatches(matches, query, source),
	)

	// disabling captures still works when there are multiple captures on a
	// single node.
	query.DisableCapture("name2")
	matches = cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(0,
				fmtCapture("name1", "foo"),
				fmtCapture("name3", "foo"),
				fmtCapture("body1", "{ return 1; }"),
				fmtCapture("body2", "{ return 1; }"),
			),
		},
		collectMatches(matches, query, source),
	)
}

func TestQueryMatchesWithCapturedWildcardAtRoot(t *testing.T) {
	language := getLanguage("python")
	query, err := NewQuery(
		language,
		`
		; captured wildcard at the root
		(_ [
			(except_clause (block) @block)
			(finally_clause (block) @block)
		]) @stmt

		[
			(while_statement (block) @block)
			(if_statement (block) @block)

			; captured wildcard at the root within an alternation
			(_ [
				(else_clause (block) @block)
				(elif_clause (block) @block)
			])

			(try_statement (block) @block)
			(for_statement (block) @block)
		] @stmt
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := strings.TrimSpace(`
        for i in j:
            while True:
                if a:
                    print b
                elif c:
                    print d
                else:
                    try:
                        print f
                    except:
                        print g
                    finally:
                        print h
            else:
                print i
	`)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	type captureNamesAndRows struct {
		Name string
		Kind string
		Row  uint
	}

	matchCaptureNamesAndRows := make([][]captureNamesAndRows, 0)

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	for match := matches.Next(); match != nil; match = matches.Next() {
		captures := make([]captureNamesAndRows, 0)
		for _, capture := range match.Captures {
			captures = append(captures, captureNamesAndRows{
				query.CaptureNames()[capture.Index],
				capture.Node.Kind(),
				capture.Node.StartPosition().Row,
			})
		}

		matchCaptureNamesAndRows = append(matchCaptureNamesAndRows, captures)
	}

	assert.Equal(
		t,
		[][]captureNamesAndRows{
			{{"stmt", "for_statement", 0}, {"block", "block", 1}},
			{{"stmt", "while_statement", 1}, {"block", "block", 2}},
			{{"stmt", "if_statement", 2}, {"block", "block", 3}},
			{{"stmt", "if_statement", 2}, {"block", "block", 5}},
			{{"stmt", "if_statement", 2}, {"block", "block", 7}},
			{{"stmt", "try_statement", 7}, {"block", "block", 8}},
			{{"stmt", "try_statement", 7}, {"block", "block", 10}},
			{{"stmt", "try_statement", 7}, {"block", "block", 12}},
			{{"stmt", "while_statement", 1}, {"block", "block", 14}},
		},
		matchCaptureNamesAndRows,
	)
}

func TestQueryMatchesWithNoCaptures(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(identifier)
		(string) @s
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a = 'hi';
		b = 'bye';
		`,
		[]formattedMatch{
			fmtMatch(0),
			fmtMatch(1, fmtCapture("s", "'hi'")),
			fmtMatch(0),
			fmtMatch(1, fmtCapture("s", "'bye'")),
		},
	)
}

func TestQueryMatchesWithRepeatedFields(t *testing.T) {
	language := getLanguage("c")
	query, err := NewQuery(
		language,
		`
		(field_declaration declarator: (field_identifier) @field)
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		struct S {
			int a, b, c;
		};
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("field", "a")),
			fmtMatch(0, fmtCapture("field", "b")),
			fmtMatch(0, fmtCapture("field", "c")),
		},
	)
}

func TestQueryMatchesWithDeeplyNestedPatternsWithFields(t *testing.T) {
	language := getLanguage("python")
	query, err := NewQuery(
		language,
		`
		(call
			function: (_) @func
			arguments: (_) @args)
		(call
			function: (attribute
				object: (_) @receiver
				attribute: (identifier) @method)
			arguments: (argument_list))

		; These don't match anything, but they require additional
		; states to keep track of their captures.
		(call
			function: (_) @fn
			arguments: (argument_list
				(keyword_argument
					name: (identifier) @name
					value: (_) @val) @arg) @args) @call
		(call
			function: (identifier) @fn
			(#eq? @fn "super")) @super_call
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		a(1).b(2).c(3).d(4).e(5).f(6).g(7).h(8)
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("func", "a"), fmtCapture("args", "(1)")),
			fmtMatch(0, fmtCapture("func", "a(1).b"), fmtCapture("args", "(2)")),
			fmtMatch(1, fmtCapture("receiver", "a(1)"), fmtCapture("method", "b")),
			fmtMatch(0, fmtCapture("func", "a(1).b(2).c"), fmtCapture("args", "(3)")),
			fmtMatch(1, fmtCapture("receiver", "a(1).b(2)"), fmtCapture("method", "c")),
			fmtMatch(0, fmtCapture("func", "a(1).b(2).c(3).d"), fmtCapture("args", "(4)")),
			fmtMatch(1, fmtCapture("receiver", "a(1).b(2).c(3)"), fmtCapture("method", "d")),
			fmtMatch(0, fmtCapture("func", "a(1).b(2).c(3).d(4).e"), fmtCapture("args", "(5)")),
			fmtMatch(1, fmtCapture("receiver", "a(1).b(2).c(3).d(4)"), fmtCapture("method", "e")),
			fmtMatch(0, fmtCapture("func", "a(1).b(2).c(3).d(4).e(5).f"), fmtCapture("args", "(6)")),
			fmtMatch(1, fmtCapture("receiver", "a(1).b(2).c(3).d(4).e(5)"), fmtCapture("method", "f")),
			fmtMatch(0, fmtCapture("func", "a(1).b(2).c(3).d(4).e(5).f(6).g"), fmtCapture("args", "(7)")),
			fmtMatch(1, fmtCapture("receiver", "a(1).b(2).c(3).d(4).e(5).f(6)"), fmtCapture("method", "g")),
			fmtMatch(0, fmtCapture("func", "a(1).b(2).c(3).d(4).e(5).f(6).g(7).h"), fmtCapture("args", "(8)")),
			fmtMatch(1, fmtCapture("receiver", "a(1).b(2).c(3).d(4).e(5).f(6).g(7)"), fmtCapture("method", "h")),
		},
	)
}

func TestQueryMatchesWithIndefiniteStepContainingNoCaptures(t *testing.T) {
	// This pattern depends on the field declarations within the
	// struct's body, but doesn't capture anything within the body.
	// It demonstrates that internally, state-splitting needs to occur
	// for each field declaration within the body, in order to avoid
	// prematurely failing if the first field does not match.
	//
	// https://github.com/tree-sitter/tree-sitter/issues/937
	language := getLanguage("c")
	query, err := NewQuery(
		language,
		`
		(struct_specifier
			name: (type_identifier) @name
			body: (field_declaration_list
				(field_declaration
					type: (union_specifier))))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	assertQueryMatches(
		t,
		language,
		query,
		`
		struct LacksUnionField {
			int a;
			struct {
				B c;
			} d;
			G *h;
		};

		struct HasUnionField {
			int a;
			struct {
				B c;
			} d;
			union {
				bool e;
				float f;
			} g;
			G *h;
		};
		`,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("name", "HasUnionField")),
		},
	)
}

func TestQueryCapturesBasic(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(pair
			key: _ @method.def
			(function_expression
				name: (identifier) @method.alias))

		(variable_declarator
			name: _ @function.def
			value: (function_expression
				name: (identifier) @function.alias))

		":" @delimiter
		"=" @operator
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	source := `
		  a({
			bc: function de() {
			  const fg = function hi() {}
			},
			jk: function lm() {
			  const no = function pq() {}
			},
		  });
		`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(2, fmtCapture("delimiter", ":")),
			fmtMatch(0, fmtCapture("method.def", "bc"), fmtCapture("method.alias", "de")),
			fmtMatch(3, fmtCapture("operator", "=")),
			fmtMatch(1, fmtCapture("function.def", "fg"), fmtCapture("function.alias", "hi")),
			fmtMatch(2, fmtCapture("delimiter", ":")),
			fmtMatch(0, fmtCapture("method.def", "jk"), fmtCapture("method.alias", "lm")),
			fmtMatch(3, fmtCapture("operator", "=")),
			fmtMatch(1, fmtCapture("function.def", "no"), fmtCapture("function.alias", "pq")),
		},
		collectMatches(matches, query, source),
	)

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedCapture{
			{"method.def", "bc"},
			{"delimiter", ":"},
			{"method.alias", "de"},
			{"function.def", "fg"},
			{"operator", "="},
			{"function.alias", "hi"},
			{"method.def", "jk"},
			{"delimiter", ":"},
			{"method.alias", "lm"},
			{"function.def", "no"},
			{"operator", "="},
			{"function.alias", "pq"},
		},
		collectCaptures(captures, query, source),
	)
}

func TestQueryCapturesWithTextConditions(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		((identifier) @constant
			(#match? @constant "^[A-Z]{2,}$"))

			((identifier) @constructor
			(#match? @constructor "^[A-Z]"))

		((identifier) @function.builtin
			(#eq? @function.builtin "require"))

		((identifier) @variable.builtin
			(#any-of? @variable.builtin
					"arguments"
					"module"
					"console"
					"window"
					"document"))

		((identifier) @variable
			(#not-match? @variable "^(lambda|load)$"))
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := `
          toad
          load
          panda
          lambda
          const ab = require('./ab');
          new Cd(EF);
          document;
          module;
          console;
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedCapture{
			{"variable", "toad"},
			{"variable", "panda"},
			{"variable", "ab"},
			{"function.builtin", "require"},
			{"variable", "require"},
			{"constructor", "Cd"},
			{"variable", "Cd"},
			{"constant", "EF"},
			{"constructor", "EF"},
			{"variable", "EF"},
			{"variable.builtin", "document"},
			{"variable", "document"},
			{"variable.builtin", "module"},
			{"variable", "module"},
			{"variable.builtin", "console"},
			{"variable", "console"},
		},
		collectCaptures(captures, query, source),
	)
}

func TestQueryCapturesWithPredicates(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		((call_expression (identifier) @foo)
			(#set! name something)
			(#set! cool)
			(#something! @foo omg))

		((property_identifier) @bar
			(#is? cool)
			(#is-not? name something))
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	strptr := func(s string) *string { return &s }
	uintptr := func(i uint) *uint { return &i }

	assert.Equal(
		t,
		[]QueryProperty{
			NewQueryProperty("name", strptr("something"), nil),
			NewQueryProperty("cool", nil, nil),
		},
		query.PropertySettings(0),
	)
	assert.Equal(
		t,
		[]QueryPredicate{
			{
				Operator: "something!",
				Args: []QueryPredicateArg{
					{CaptureId: uintptr(0)},
					{String: strptr("omg")},
				},
			},
		},
		query.GeneralPredicates(0),
	)
	assert.Equal(
		t,
		[]QueryProperty{},
		query.PropertySettings(1),
	)
	assert.Equal(
		t,
		[]PropertyPredicate{
			{NewQueryProperty("cool", nil, nil), true},
			{NewQueryProperty("name", strptr("something"), nil), false},
		},
		query.PropertyPredicates(1),
	)

	source := "const a = window.b"
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	query, err = NewQuery(
		language,
		`
		((identifier) @variable.builtin
			(#match? @variable.builtin "^(arguments|module|console|window|document)$")
			(#is-not? local))
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("variable.builtin", "window")),
		},
		collectMatches(matches, query, source),
	)
}

func TestQueryCapturesWithQuotedPredicateArgs(t *testing.T) {
	language := getLanguage("javascript")

	// Double-quoted strings can contain:
	// * special escape sequences like \n and \r
	// * escaped double quotes with \*
	// * literal backslashes with \\
	query, err := NewQuery(
		language,
		`
		((call_expression (identifier) @foo)
			(#set! one "\"something\ngreat\""))

		((identifier)
			(#set! two "\\s(\r?\n)*$"))

		((function_declaration)
			(#set! three "\"something\ngreat\""))
		`,
	)
	defer query.Close()
	assert.Nil(t, err)

	strptr := func(s string) *string { return &s }

	assert.Equal(
		t,
		[]QueryProperty{
			NewQueryProperty("one", strptr("\"something\ngreat\""), nil),
		},
		query.PropertySettings(0),
	)
	assert.Equal(
		t,
		[]QueryProperty{
			NewQueryProperty("two", strptr("\\s(\r?\n)*$"), nil),
		},
		query.PropertySettings(1),
	)
	assert.Equal(
		t,
		[]QueryProperty{
			NewQueryProperty("three", strptr("\"something\ngreat\""), nil),
		},
		query.PropertySettings(2),
	)
}

func TestQueryCapturesWithDuplicates(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(variable_declarator
			name: (identifier) @function
			value: (function_expression))

		(identifier) @variable
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := `
          var x = function() {};
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	assert.Nil(t, err)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedCapture{
			{"function", "x"},
			{"variable", "x"},
		},
		collectCaptures(captures, query, source),
	)
}

func TestQueryCapturesWithManyNestedResultsWithoutFields(t *testing.T) {
	language := getLanguage("javascript")

	// Search for key-value pairs whose values are anonymous functions.
	query, err := NewQuery(
		language,
		`
		(pair
			key: _ @method-def
			(arrow_function))

		":" @colon
		"," @comma
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	// The `pair` node for key `y` does not match any pattern, but inside of
	// its value, it contains many other `pair` nodes that do match the pattern.
	// The match for the *outer* pair should be terminated *before* descending into
	// the object value, so that we can avoid needing to buffer all of the inner
	// matches.
	methodCount := 50
	source := "x = { y: {\n"
	for i := 0; i < methodCount; i++ {
		source += fmt.Sprintf("    method%d: $ => null,\n", i)
	}
	source += "}};\n"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	capturesResult := collectCaptures(captures, query, source)

	assert.Equal(
		t,
		[]formattedCapture{
			{"colon", ":"},
			{"method-def", "method0"},
			{"colon", ":"},
			{"comma", ","},
			{"method-def", "method1"},
			{"colon", ":"},
			{"comma", ","},
			{"method-def", "method2"},
			{"colon", ":"},
			{"comma", ","},
			{"method-def", "method3"},
			{"colon", ":"},
			{"comma", ","},
		},
		capturesResult[:13],
	)

	// Ensure that we don't drop matches because of needing to buffer too many.
	assert.Equal(t, 1+3*methodCount, len(capturesResult))
}

func TestQueryCapturesWithManyNestedResultsWithFields(t *testing.T) {
	language := getLanguage("javascript")

	// Search expressions like `a ? a.b : null`
	query, err := NewQuery(
		language,
		`
		((ternary_expression
			condition: (identifier) @left
			consequence: (member_expression
				object: (identifier) @right)
			alternative: (null))
			(#eq? @left @right))
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	// The outer expression does not match the pattern, but the consequence of the ternary
	// is an object that *does* contain many occurrences of the pattern.
	count := 50
	source := "a ? {"
	for i := 0; i < count; i++ {
		source += fmt.Sprintf("  x: y%d ? y%d.z : null,\n", i, i)
	}
	source += "} : null;\n"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	capturesResult := collectCaptures(captures, query, source)

	assert.Equal(
		t,
		[]formattedCapture{
			{"left", "y0"},
			{"right", "y0"},
			{"left", "y1"},
			{"right", "y1"},
			{"left", "y2"},
			{"right", "y2"},
			{"left", "y3"},
			{"right", "y3"},
			{"left", "y4"},
			{"right", "y4"},
			{"left", "y5"},
			{"right", "y5"},
			{"left", "y6"},
			{"right", "y6"},
			{"left", "y7"},
			{"right", "y7"},
			{"left", "y8"},
			{"right", "y8"},
			{"left", "y9"},
			{"right", "y9"},
		},
		capturesResult[:20],
	)

	// Ensure that we don't drop matches because of needing to buffer too many.
	assert.Equal(t, 2*count, len(capturesResult))
}

func TestQueryCapturesWithTooManyNestedResults(t *testing.T) {
	language := getLanguage("javascript")

	// Search for method calls in general, and also method calls with a template string
	// in place of an argument list (aka "tagged template strings") in particular.
	//
	// This second pattern, which looks for the tagged template strings, is expensive to
	// use with the `captures()` method, because:
	// 1. When calling `captures`, all of the captures must be returned in order of their
	//    appearance.
	// 2. This pattern captures the root `call_expression`.
	// 3. This pattern's result also depends on the final child (the template string).
	// 4. In between the `call_expression` and the possible `template_string`, there can be an
	//    arbitrarily deep subtree.
	//
	// This means that, if any patterns match *after* the initial `call_expression` is
	// captured, but before the final `template_string` is found, those matches must
	// be buffered, in order to prevent captures from being returned out-of-order.
	query, err := NewQuery(
		language,
		`
		;; easy 👇
		(call_expression
			function: (member_expression
			property: (property_identifier) @method-name))

		;; hard 👇
		(call_expression
			function: (member_expression
			property: (property_identifier) @template-tag)
			arguments: (template_string)) @template-call
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	// There are a *lot* of matches in between the beginning of the outer `call_expression`
	// (the call to `a(...).f`), which starts at the beginning of the file, and the final
	// template string, which occurs at the end of the file. The query algorithm imposes a
	// limit on the total number of matches which can be buffered at a time. But we don't
	// want to neglect the inner matches just because of the expensive outer match, so we
	// abandon the outer match (which would have captured `f` as a `template-tag`).
	source := strings.TrimSpace(
		" a(b => {\n" +
			"     b.c0().d0 `😄`;\n" +
			"     b.c1().d1 `😄`;\n" +
			"     b.c2().d2 `😄`;\n" +
			"     b.c3().d3 `😄`;\n" +
			"     b.c4().d4 `😄`;\n" +
			"     b.c5().d5 `😄`;\n" +
			"     b.c6().d6 `😄`;\n" +
			"     b.c7().d7 `😄`;\n" +
			"     b.c8().d8 `😄`;\n" +
			"     b.c9().d9 `😄`;\n" +
			" }).e().f ``;\n",
	)

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	cursor.SetMatchLimit(32)
	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	capturesResult := collectCaptures(captures, query, source)

	assert.Equal(
		t,
		[]formattedCapture{
			{"template-call", "b.c0().d0 `😄`"},
			{"method-name", "c0"},
			{"method-name", "d0"},
			{"template-tag", "d0"},
		},
		capturesResult[:4],
	)
	assert.Equal(
		t,
		[]formattedCapture{
			{"template-call", "b.c9().d9 `😄`"},
			{"method-name", "c9"},
			{"method-name", "d9"},
			{"template-tag", "d9"},
		},
		capturesResult[36:40],
	)
	assert.Equal(
		t,
		[]formattedCapture{
			{"method-name", "e"},
			{"method-name", "f"},
		},
		capturesResult[40:],
	)
}

func TestQueryCapturesWithDefinitePatternContainingManyNestedMatches(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(array
			"[" @l-bracket
			"]" @r-bracket)

		"." @dot
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	// The '[' node must be returned before all of the '.' nodes,
	// even though its pattern does not finish until the ']' node
	// at the end of the document. But because the '[' is definite,
	// it can be returned before the pattern finishes matching.
	source := `
        [
            a.b.c.d.e.f.g.h.i,
            a.b.c.d.e.f.g.h.i,
            a.b.c.d.e.f.g.h.i,
            a.b.c.d.e.f.g.h.i,
            a.b.c.d.e.f.g.h.i,
        ]
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	capturesResult := collectCaptures(captures, query, source)

	expected := make([]formattedCapture, 42)
	expected[0] = formattedCapture{"l-bracket", "["}
	for i := 1; i < 41; i++ {
		expected[i] = formattedCapture{"dot", "."}
	}
	expected[41] = formattedCapture{"r-bracket", "]"}

	assert.Equal(t, expected, capturesResult)
}

func TestQueryCapturesOrderedByBothStartAndEndPositions(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(call_expression) @call
		(member_expression) @member
		(identifier) @variable
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := `
		a.b(c.d().e).f;
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedCapture{
			{"member", "a.b(c.d().e).f"},
			{"call", "a.b(c.d().e)"},
			{"member", "a.b"},
			{"variable", "a"},
			{"member", "c.d().e"},
			{"call", "c.d()"},
			{"member", "c.d"},
			{"variable", "c"},
		},
		collectCaptures(captures, query, source),
	)
}

func TestQueryCapturesWithMatchesRemoved(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(binary_expression
			left: (identifier) @left
			operator: _ @op
			right: (identifier) @right)
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := `
		a === b && c > d && e < f;
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	capturedStrings := make([]string, 0)
	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	for match, index := captures.Next(); match != nil; match, index = captures.Next() {
		capture := match.Captures[index]
		text := capture.Node.Utf8Text([]byte(source))
		if text == "a" {
			match.Remove()
			continue
		}
		capturedStrings = append(capturedStrings, text)
	}

	assert.Equal(t, []string{"c", ">", "d", "e", "<", "f"}, capturedStrings)
}

func TestQueryCapturesWithMatchesRemovedBeforeTheyFinish(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(namespace_import
			"*" @star
			"as" @as
			(identifier) @identifier)
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := `
		import * as name from 'module-name';
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	capturedStrings := make([]string, 0)
	captures := cursor.Captures(query, tree.RootNode(), []byte(source))
	for match, index := captures.Next(); match != nil; match, index = captures.Next() {
		capture := match.Captures[index]
		text := capture.Node.Utf8Text([]byte(source))
		if text == "as" {
			match.Remove()
			continue
		}
		capturedStrings = append(capturedStrings, text)
	}

	// .remove() removes the match before it is finished. The identifier
	// "name" is part of this match, so we expect that removing the "as"
	// capture from the match should prevent "name" from matching:
	assert.Equal(t, []string{"*"}, capturedStrings)
}

func TestQueryCapturesAndMatchesIteratorsAreFused(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(comment) @comment
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := `
		// one
		// two
		// three
		/* unfinished
	`

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	captures := cursor.Captures(query, tree.RootNode(), []byte(source))

	capture, _ := captures.Next()
	assert.EqualValues(t, 0, capture.Captures[0].Index)
	capture, _ = captures.Next()
	assert.EqualValues(t, 0, capture.Captures[0].Index)
	capture, _ = captures.Next()
	assert.EqualValues(t, 0, capture.Captures[0].Index)
	capture, _ = captures.Next()
	assert.Nil(t, capture)
	capture, _ = captures.Next()
	assert.Nil(t, capture)
	capture, _ = captures.Next()
	assert.Nil(t, capture)

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))

	assert.EqualValues(t, 0, matches.Next().Captures[0].Index)
	assert.EqualValues(t, 0, matches.Next().Captures[0].Index)
	assert.EqualValues(t, 0, matches.Next().Captures[0].Index)
	assert.Nil(t, matches.Next())
	assert.Nil(t, matches.Next())
	assert.Nil(t, matches.Next())
}

func TestQueryStartEndByteForPattern(t *testing.T) {
	language := getLanguage("javascript")

	patterns1 := strings.TrimSpace(`
"+" @operator
"-" @operator
"*" @operator
"=" @operator
"=>" @operator
	`)

	patterns2 := strings.TrimSpace(`
(identifier) @a
(string) @b
	`)

	patterns3 := strings.TrimSpace(`
((identifier) @b (#match? @b i))
(function_declaration name: (identifier) @c)
(method_definition name: (property_identifier) @d)
	`)

	source := patterns1 + patterns2 + patterns3

	query, err := NewQuery(language, source)
	assert.Nil(t, err)
	defer query.Close()

	assert.EqualValues(t, 0, query.StartByteForPattern(0))
	assert.EqualValues(t, len("\"+\" @operator\n"), query.EndByteForPattern(0))
	assert.EqualValues(t, len(patterns1), query.StartByteForPattern(5))
	assert.EqualValues(t, len(patterns1)+len("(identifier) @a\n"), query.EndByteForPattern(5))
	assert.EqualValues(t, len(patterns1)+len(patterns2), query.StartByteForPattern(7))
	assert.EqualValues(t, len(patterns1)+len(patterns2)+len("((identifier) @b (#match? @b i))\n"), query.EndByteForPattern(7))
}

func TestQueryCaptureNames(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		(if_statement
			condition: (parenthesized_expression (binary_expression
				left: _ @left-operand
				operator: "||"
				right: _ @right-operand))
			consequence: (statement_block) @body)

		(while_statement
			condition: _ @loop-condition)
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	assert.Equal(t, []string{"left-operand", "right-operand", "body", "loop-condition"}, query.CaptureNames())
}

func TestQueryWithNoPatterns(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "")
	assert.Nil(t, err)
	defer query.Close()

	assert.EqualValues(t, 0, query.PatternCount())
	assert.Equal(t, []string{}, query.CaptureNames())
}

func TestQueryComments(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
		; this is my first comment
		; i have two comments here
		(function_declaration
			; there is also a comment here
			; and here
			name: (identifier) @fn-name)
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := "function one() { }"

	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, []formattedMatch{fmtMatch(0, fmtCapture("fn-name", "one"))}, collectMatches(matches, query, source))
}

func TestQueryDisablePattern(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(
		language,
		`
			(function_declaration
				name: (identifier) @name)
			(function_declaration
				body: (statement_block) @body)
			(class_declaration
				name: (identifier) @name)
			(class_declaration
				body: (class_body) @body)
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	// disable the patterns that match names
	query.DisablePattern(0)
	query.DisablePattern(2)

	source := "class A { constructor() {} } function b() { return 1; }"
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(
		t,
		[]formattedMatch{
			fmtMatch(3, fmtCapture("body", "{ constructor() {} }")),
			fmtMatch(1, fmtCapture("body", "{ return 1; }")),
		},
		collectMatches(matches, query, source),
	)
}

func TestQueryAlternativePredicatePrefix(t *testing.T) {
	language := getLanguage("c")
	query, err := NewQuery(
		language,
		`
		((call_expression
			function: (identifier) @keyword
			arguments: (argument_list
						(string_literal) @function))
		 (.eq? @keyword "DEFUN"))
		`,
	)
	assert.Nil(t, err)
	defer query.Close()

	source := `
		DEFUN ("identity", Fidentity, Sidentity, 1, 1, 0,
			   doc: /* Return the argument unchanged.  */
			   attributes: const)
		  (Lisp_Object arg)
		{
		  return arg;
		}
	`

	assertQueryMatches(
		t,
		language,
		query,
		source,
		[]formattedMatch{fmtMatch(0, fmtCapture("keyword", "DEFUN"), fmtCapture("function", "\"identity\""))},
	)
}

func TestQueryIsPatternGuaranteedAtStep(t *testing.T) {
	type rbs struct {
		substring  string
		isDefinite bool
	}
	type row struct {
		language           *Language
		description        string
		pattern            string
		resultsBySubstring []rbs
	}
	rows := []row{
		{
			description:        "no guaranteed steps",
			language:           getLanguage("python"),
			pattern:            "(expression_statement (string))",
			resultsBySubstring: []rbs{{"expression_statement", false}, {"string", false}},
		},
		{
			description:        "all guaranteed steps",
			language:           getLanguage("javascript"),
			pattern:            `(object "{" "}")`,
			resultsBySubstring: []rbs{{"object", false}, {"{", true}, {"}", true}},
		},
		{
			description:        "a fallible step that is optional",
			language:           getLanguage("javascript"),
			pattern:            `(object "{" (identifier)? @foo "}")`,
			resultsBySubstring: []rbs{{"object", false}, {"{", true}, {"(identifier)?", false}, {"}", true}},
		},
		{
			description:        "multiple fallible steps that are optional",
			language:           getLanguage("javascript"),
			pattern:            `(object "{" (identifier)? @id1 ("," (identifier) @id2)? "}")`,
			resultsBySubstring: []rbs{{"object", false}, {"{", true}, {"(identifier)? @id1", false}, {"\",\"", false}, {"}", true}},
		},
		{
			description:        "guaranteed step after fallible step",
			language:           getLanguage("javascript"),
			pattern:            `(pair (property_identifier) ":")`,
			resultsBySubstring: []rbs{{"pair", false}, {"property_identifier", false}, {":", true}},
		},
		{
			description:        "fallible step in between two guaranteed steps",
			language:           getLanguage("javascript"),
			pattern:            `(ternary_expression condition: (_) "?" consequence: (call_expression) ":" alternative: (_))`,
			resultsBySubstring: []rbs{{"condition:", false}, {"\"?\"", false}, {"consequence:", false}, {"\":\"", true}, {"alternative:", true}},
		},
		{
			description:        "one guaranteed step after a repetition",
			language:           getLanguage("javascript"),
			pattern:            `(object "{" (_) "}")`,
			resultsBySubstring: []rbs{{"object", false}, {"{", false}, {"(_)", false}, {"}", true}},
		},
		{
			description:        "guaranteed steps after multiple repetitions",
			language:           getLanguage("json"),
			pattern:            `(object "{" (pair) "," (pair) "," (_) "}")`,
			resultsBySubstring: []rbs{{"object", false}, {"{", false}, {"(pair) \",\" (pair)", false}, {"(pair) \",\" (_)", false}, {"\",\" (_)", false}, {"(_)", true}, {"}", true}},
		},
		{
			description:        "a guaranteed step with a field",
			language:           getLanguage("javascript"),
			pattern:            `(binary_expression left: (expression) right: (_))`,
			resultsBySubstring: []rbs{{"binary_expression", false}, {"(expression)", false}, {"(_)", true}},
		},
		{
			description:        "multiple guaranteed steps with fields",
			language:           getLanguage("javascript"),
			pattern:            `(function_declaration name: (identifier) body: (statement_block))`,
			resultsBySubstring: []rbs{{"function_declaration", false}, {"identifier", true}, {"statement_block", true}},
		},
		{
			description:        "nesting, one guaranteed step",
			language:           getLanguage("javascript"),
			pattern:            `(function_declaration name: (identifier) body: (statement_block "{" (expression_statement) "}"))`,
			resultsBySubstring: []rbs{{"function_declaration", false}, {"identifier", false}, {"statement_block", false}, {"{", false}, {"expression_statement", false}, {"}", true}},
		},
		{
			description:        "a guaranteed step after some deeply nested hidden nodes",
			language:           getLanguage("ruby"),
			pattern:            `(singleton_class value: (constant) "end")`,
			resultsBySubstring: []rbs{{"singleton_class", false}, {"constant", false}, {"end", true}},
		},
		{
			description:        "nesting, no guaranteed steps",
			language:           getLanguage("javascript"),
			pattern:            `(call_expression function: (member_expression property: (property_identifier) @template-tag) arguments: (template_string)) @template-call`,
			resultsBySubstring: []rbs{{"property_identifier", false}, {"template_string", false}},
		},
		{
			description:        "a guaranteed step after a nested node",
			language:           getLanguage("javascript"),
			pattern:            `(subscript_expression object: (member_expression object: (identifier) @obj property: (property_identifier) @prop) "[")`,
			resultsBySubstring: []rbs{{"identifier", false}, {"property_identifier", false}, {"[", true}},
		},
		{
			description:        "a step that is fallible due to a predicate",
			language:           getLanguage("javascript"),
			pattern:            `(subscript_expression object: (member_expression object: (identifier) @obj property: (property_identifier) @prop) "[" (#match? @prop "foo"))`,
			resultsBySubstring: []rbs{{"identifier", false}, {"property_identifier", false}, {"[", true}},
		},
		{
			description:        "alternation where one branch has guaranteed steps",
			language:           getLanguage("javascript"),
			pattern:            `[(unary_expression (identifier)) (call_expression function: (_) arguments: (_)) (binary_expression right: (call_expression))]`,
			resultsBySubstring: []rbs{{"identifier", false}, {"right:", false}, {"function:", true}, {"arguments:", true}},
		},
		{
			description:        "guaranteed step at the end of an aliased parent node",
			language:           getLanguage("ruby"),
			pattern:            `(method_parameters "(" (identifier) @id")")`,
			resultsBySubstring: []rbs{{"\"(\"", false}, {"(identifier)", false}, {"\")\"", true}},
		},
		{
			description:        "long, but not too long to analyze",
			language:           getLanguage("javascript"),
			pattern:            `(object "{" (pair) (pair) (pair) (pair) "}")`,
			resultsBySubstring: []rbs{{"\"{\"", false}, {"(pair)", false}, {"(pair) \"}\"", false}, {"\"}\"", true}},
		},
		{
			description:        "too long to analyze",
			language:           getLanguage("javascript"),
			pattern:            `(object "{" (pair) (pair) (pair) (pair) (pair) (pair) (pair) (pair) (pair) (pair) (pair) (pair) "}")`,
			resultsBySubstring: []rbs{{"\"{\"", false}, {"(pair)", false}, {"(pair) \"}\"", false}, {"\"}\"", false}},
		},
		{
			description:        "hidden nodes that have several fields",
			language:           getLanguage("java"),
			pattern:            `(method_declaration name: (identifier))`,
			resultsBySubstring: []rbs{{"name:", true}},
		},
		{
			description:        "top-level non-terminal extra nodes",
			language:           getLanguage("ruby"),
			pattern:            `(heredoc_body (interpolation) (heredoc_end) @end)`,
			resultsBySubstring: []rbs{{"(heredoc_body", false}, {"(interpolation)", false}, {"(heredoc_end)", true}},
		},
	}

	for _, row := range rows {
		query, err := NewQuery(row.language, row.pattern)
		assert.Nil(t, err)
		defer query.Close()

		for _, rbs := range row.resultsBySubstring {
			offset := uint(strings.Index(row.pattern, rbs.substring))
			assert.Equal(
				t,
				rbs.isDefinite,
				query.IsPatternGuaranteedAtStep(offset),
				fmt.Sprintf(
					"Description: %s, Pattern: %s, substring: %s, expected is_definite to be %t\n",
					row.description,
					strings.Join(strings.Fields(row.pattern), " "),
					rbs.substring,
					rbs.isDefinite,
				),
			)
		}
	}
}

func TestQueryIsPatternRooted(t *testing.T) {
	type row struct {
		description string
		pattern     string
		isRooted    bool
	}
	rows := []row{
		{
			description: "simple token",
			pattern:     "(identifier)",
			isRooted:    true,
		},
		{
			description: "simple non-terminal",
			pattern:     "(function_definition name: (identifier))",
			isRooted:    true,
		},
		{
			description: "alternative of many tokens",
			pattern:     `["if" "def" (identifier) (comment)]`,
			isRooted:    true,
		},
		{
			description: "alternative of many non-terminals",
			pattern: `[
				(function_definition name: (identifier))
				(class_definition name: (identifier))
				(block)
			]`,
			isRooted: true,
		},
		{
			description: "two siblings",
			pattern:     `("{" "}")`,
			isRooted:    false,
		},
		{
			description: "top-level repetition",
			pattern:     `(comment)*`,
			isRooted:    false,
		},
		{
			description: "alternative where one option has two siblings",
			pattern: `[
                (block)
                (class_definition)
                ("(" ")")
                (function_definition)
            ]`,
			isRooted: false,
		},
		{
			description: "alternative where one option has a top-level repetition",
			pattern: `[
                (block)
                (class_definition)
                (comment)*
                (function_definition)
            ]`,
			isRooted: false,
		},
	}

	language := getLanguage("python")
	for _, row := range rows {
		query, err := NewQuery(language, row.pattern)
		assert.Nil(t, err)
		defer query.Close()
		assert.Equal(
			t,
			row.isRooted,
			query.IsPatternRooted(0),
			fmt.Sprintf(
				"Description: %s, Pattern: %s\n",
				row.description,
				strings.Join(strings.Fields(row.pattern), " "),
			),
		)
	}
}

func TestQueryIsPatternNonLocal(t *testing.T) {
	type row struct {
		description string
		pattern     string
		language    *Language
		isNonLocal  bool
	}
	rows := []row{
		{
			description: "simple token",
			pattern:     "(identifier)",
			language:    getLanguage("python"),
			isNonLocal:  false,
		},
		{
			description: "siblings that can occur in an argument list",
			pattern:     "((identifier) (identifier))",
			language:    getLanguage("python"),
			isNonLocal:  true,
		},
		{
			description: "siblings that can occur in a statement block",
			pattern:     "((return_statement) (return_statement))",
			language:    getLanguage("python"),
			isNonLocal:  true,
		},
		{
			description: "siblings that can occur in a source file",
			pattern:     "((function_definition) (class_definition))",
			language:    getLanguage("python"),
			isNonLocal:  true,
		},
		{
			description: "siblings that can't occur in any repetition",
			pattern:     `("{" "}")`,
			language:    getLanguage("python"),
			isNonLocal:  false,
		},
		{
			description: "siblings that can't occur in any repetition, wildcard root",
			pattern:     `(_ "{" "}") @foo`,
			language:    getLanguage("javascript"),
			isNonLocal:  false,
		},
		{
			description: "siblings that can occur in a class body, wildcard root",
			pattern:     `(_ (method_definition) (method_definition)) @foo`,
			language:    getLanguage("javascript"),
			isNonLocal:  true,
		},
		{
			description: "top-level repetitions that can occur in a class body",
			pattern:     `(method_definition)+ @foo`,
			language:    getLanguage("javascript"),
			isNonLocal:  true,
		},
		{
			description: "top-level repetitions that can occur in a statement block",
			pattern:     `(return_statement)+ @foo`,
			language:    getLanguage("javascript"),
			isNonLocal:  true,
		},
		{
			description: "rooted pattern that can occur in a statement block",
			pattern:     `(return_statement) @foo`,
			language:    getLanguage("javascript"),
			isNonLocal:  false,
		},
	}

	for _, row := range rows {
		query, err := NewQuery(row.language, row.pattern)
		assert.Nil(t, err)
		defer query.Close()
		assert.Equal(
			t,
			row.isNonLocal,
			query.IsPatternNonLocal(0),
			fmt.Sprintf(
				"Description: %s, Pattern: %s\n",
				row.description,
				strings.Join(strings.Fields(row.pattern), " "),
			),
		)
	}
}

func TestCaptureQuantifiers(t *testing.T) {
	type captureQuantifier struct {
		pattern    uint
		capture    string
		quantifier CaptureQuantifier
	}
	type row struct {
		description        string
		language           *Language
		pattern            string
		captureQuantifiers []captureQuantifier
	}
	rows := []row{
		{
			description: "Top level capture",
			language:    getLanguage("python"),
			pattern:     "(module) @mod",
			captureQuantifiers: []captureQuantifier{
				{0, "mod", CaptureQuantifierOne},
			},
		},
		{
			description: "Nested list capture",
			language:    getLanguage("javascript"),
			pattern:     "(array (_)* @elems) @array",
			captureQuantifiers: []captureQuantifier{
				{0, "array", CaptureQuantifierOne},
				{0, "elems", CaptureQuantifierZeroOrMore},
			},
		},
		{
			description: "Nested non-empty list capture",
			language:    getLanguage("javascript"),
			pattern:     "(array (_)+ @elems) @array",
			captureQuantifiers: []captureQuantifier{
				{0, "array", CaptureQuantifierOne},
				{0, "elems", CaptureQuantifierOneOrMore},
			},
		},
		{
			description: "capture nested in optional pattern",
			language:    getLanguage("javascript"),
			pattern:     "(array (call_expression (arguments (_) @arg))? @call) @array",
			captureQuantifiers: []captureQuantifier{
				{0, "array", CaptureQuantifierOne},
				{0, "call", CaptureQuantifierZeroOrOne},
				{0, "arg", CaptureQuantifierZeroOrOne},
			},
		},
		{
			description: "optional capture nested in non-empty list pattern",
			language:    getLanguage("javascript"),
			pattern:     "(array (call_expression (arguments (_)? @arg))+ @call) @array",
			captureQuantifiers: []captureQuantifier{
				{0, "array", CaptureQuantifierOne},
				{0, "call", CaptureQuantifierOneOrMore},
				{0, "arg", CaptureQuantifierZeroOrMore},
			},
		},
		{
			description: "non-empty list capture nested in optional pattern",
			language:    getLanguage("javascript"),
			pattern:     "(array (call_expression (arguments (_)+ @args))? @call) @array",
			captureQuantifiers: []captureQuantifier{
				{0, "array", CaptureQuantifierOne},
				{0, "call", CaptureQuantifierZeroOrOne},
				{0, "args", CaptureQuantifierZeroOrMore},
			},
		},
		{
			description: "capture is the same in all alternatives",
			language:    getLanguage("javascript"),
			pattern: `[
				(function_declaration name:(identifier) @name)
				(call_expression function:(identifier) @name)
			]`,
			captureQuantifiers: []captureQuantifier{
				{0, "name", CaptureQuantifierOne},
			},
		},
		{
			description: "capture appears in some alternatives",
			language:    getLanguage("javascript"),
			pattern: `[
				(function_declaration name:(identifier) @name)
				(function_expression)
			] @fun`,
			captureQuantifiers: []captureQuantifier{
				{0, "fun", CaptureQuantifierOne},
				{0, "name", CaptureQuantifierZeroOrOne},
			},
		},
		{
			description: "capture has different quantifiers in alternatives",
			language:    getLanguage("javascript"),
			pattern: `[
				(call_expression arguments: (arguments (_)+ @args))
				(new_expression arguments: (arguments (_)? @args))
			] @call`,
			captureQuantifiers: []captureQuantifier{
				{0, "call", CaptureQuantifierOne},
				{0, "args", CaptureQuantifierZeroOrMore},
			},
		},
		{
			description: "siblings have different captures with different quantifiers",
			language:    getLanguage("javascript"),
			pattern:     "(call_expression (arguments (identifier)? @self (_)* @args)) @call",
			captureQuantifiers: []captureQuantifier{
				{0, "call", CaptureQuantifierOne},
				{0, "self", CaptureQuantifierZeroOrOne},
				{0, "args", CaptureQuantifierZeroOrMore},
			},
		},
		{
			description: "siblings have same capture with different quantifiers",
			language:    getLanguage("javascript"),
			pattern:     "(call_expression (arguments (identifier) @args (_)* @args)) @call",
			captureQuantifiers: []captureQuantifier{
				{0, "call", CaptureQuantifierOne},
				{0, "args", CaptureQuantifierOneOrMore},
			},
		},
		{
			description: "combined nesting, alternatives, and siblings",
			language:    getLanguage("javascript"),
			pattern: `(array
				(call_expression
					(arguments [
						(identifier) @self
						(_)+ @args
					])
				)+ @call
			) @array`,
			captureQuantifiers: []captureQuantifier{
				{0, "array", CaptureQuantifierOne},
				{0, "call", CaptureQuantifierOneOrMore},
				{0, "self", CaptureQuantifierZeroOrMore},
				{0, "args", CaptureQuantifierZeroOrMore},
			},
		},
		{
			description: "multiple patterns",
			language:    getLanguage("javascript"),
			pattern: `(function_declaration name: (identifier) @x)
				(statement_identifier) @y
				(property_identifier)+ @z
				(array (identifier)* @x)`,
			captureQuantifiers: []captureQuantifier{
				// x
				{0, "x", CaptureQuantifierOne},
				{1, "x", CaptureQuantifierZero},
				{2, "x", CaptureQuantifierZero},
				{3, "x", CaptureQuantifierZeroOrMore},
				// y
				{0, "y", CaptureQuantifierZero},
				{1, "y", CaptureQuantifierOne},
				{2, "y", CaptureQuantifierZero},
				{3, "y", CaptureQuantifierZero},
				// z
				{0, "z", CaptureQuantifierZero},
				{1, "z", CaptureQuantifierZero},
				{2, "z", CaptureQuantifierOneOrMore},
				{3, "z", CaptureQuantifierZero},
			},
		},
		{
			description: "multiple alternatives",
			language:    getLanguage("javascript"),
			pattern: `[
				(array (identifier) @x)
				(function_declaration name: (identifier)+ @x)
			]
			[
				(array (identifier) @x)
				(function_declaration name: (identifier)+ @x)
			]`,
			captureQuantifiers: []captureQuantifier{
				{0, "x", CaptureQuantifierOneOrMore},
				{1, "x", CaptureQuantifierOneOrMore},
			},
		},
	}

	for _, row := range rows {
		query, err := NewQuery(row.language, row.pattern)
		assert.Nil(t, err)
		defer query.Close()
		for _, cq := range row.captureQuantifiers {
			index, ok := query.CaptureIndexForName(cq.capture)
			assert.True(t, ok)
			assert.Equal(
				t,
				cq.quantifier,
				query.CaptureQuantifiers(cq.pattern)[index],
				fmt.Sprintf(
					"Description: %s, Pattern: %s, expected quantifier of @%s to be %v instead of %v\n",
					row.description,
					strings.Join(strings.Fields(row.pattern), " "),
					cq.capture,
					cq.quantifier,
					query.CaptureQuantifiers(cq.pattern)[index],
				),
			)
		}
	}
}

func TestQueryQuantifiedCaptures(t *testing.T) {
	type row struct {
		description string
		language    *Language
		code        string
		pattern     string
		captures    []formattedCapture
	}
	rows := []row{
		{
			description: "doc comments where all must match the prefix",
			language:    getLanguage("c"),
			code: `
/// foo
/// bar
/// baz

void main() {}

/// qux
/// quux
// quuz`,
			pattern: `((comment)+ @comment.documentation
                      (#match? @comment.documentation "^///"))`,
			captures: []formattedCapture{
				{"comment.documentation", "/// foo"},
				{"comment.documentation", "/// bar"},
				{"comment.documentation", "/// baz"},
			},
		},
		{
			description: "doc comments where one must match the prefix",
			language:    getLanguage("c"),
			code: `
/// foo
/// bar
/// baz

void main() {}

/// qux
/// quux
// quuz`,
			pattern: `((comment)+ @comment.documentation
                      (#any-match? @comment.documentation "^///"))`,
			captures: []formattedCapture{
				{"comment.documentation", "/// foo"},
				{"comment.documentation", "/// bar"},
				{"comment.documentation", "/// baz"},
				{"comment.documentation", "/// qux"},
				{"comment.documentation", "/// quux"},
				{"comment.documentation", "// quuz"},
			},
		},
	}

	for _, row := range rows {
		parser := NewParser()
		defer parser.Close()
		parser.SetLanguage(row.language)

		tree := parser.Parse([]byte(row.code), nil)
		defer tree.Close()

		query, err := NewQuery(row.language, row.pattern)
		assert.Nil(t, err)
		defer query.Close()

		cursor := NewQueryCursor()
		defer cursor.Close()

		matches := cursor.Captures(query, tree.RootNode(), []byte(row.code))
		assert.Equal(t, row.captures, collectCaptures(matches, query, row.code))
	}
}

func TestQueryMaxStartDepth(t *testing.T) {
	type row struct {
		description string
		pattern     string
		depth       uint
		matches     []formattedMatch
	}
	source := strings.TrimSpace(`
if (a1 && a2) {
    if (b1 && b2) { }
    if (c) { }
}
if (d) {
    if (e1 && e2) { }
    if (f) { }
}
	`)

	rows := []row{
		{
			description: "depth 0: match translation unit",
			depth:       0,
			pattern:     `(translation_unit) @capture`,
			matches: []formattedMatch{
				fmtMatch(0, fmtCapture("capture", source)),
			},
		},
		{
			description: "depth 0: match none",
			depth:       0,
			pattern:     `(if_statement) @capture`,
			matches:     []formattedMatch{},
		},
		{
			description: "depth 1: match 2 if statements at the top level",
			depth:       1,
			pattern:     `(if_statement) @capture`,
			matches: []formattedMatch{
				fmtMatch(0, fmtCapture("capture", "if (a1 && a2) {\n    if (b1 && b2) { }\n    if (c) { }\n}")),
				fmtMatch(0, fmtCapture("capture", "if (d) {\n    if (e1 && e2) { }\n    if (f) { }\n}")),
			},
		},
		{
			description: "depth 1 with deep pattern: match only the first if statement",
			depth:       1,
			pattern: `(if_statement
                        condition: (parenthesized_expression
                            (binary_expression)
                        )
                    ) @capture`,
			matches: []formattedMatch{
				fmtMatch(0, fmtCapture("capture", "if (a1 && a2) {\n    if (b1 && b2) { }\n    if (c) { }\n}")),
			},
		},
		{
			description: "depth 3 with deep pattern: match all if statements with a binexpr condition",
			depth:       3,
			pattern: `(if_statement
                        condition: (parenthesized_expression
                            (binary_expression)
                        )
                    ) @capture`,
			matches: []formattedMatch{
				fmtMatch(0, fmtCapture("capture", "if (a1 && a2) {\n    if (b1 && b2) { }\n    if (c) { }\n}")),
				fmtMatch(0, fmtCapture("capture", "if (b1 && b2) { }")),
				fmtMatch(0, fmtCapture("capture", "if (e1 && e2) { }")),
			},
		},
	}

	for _, row := range rows {
		language := getLanguage("c")
		parser := NewParser()
		defer parser.Close()
		parser.SetLanguage(language)

		tree := parser.Parse([]byte(source), nil)
		defer tree.Close()

		query, err := NewQuery(language, row.pattern)
		assert.Nil(t, err)
		defer query.Close()

		cursor := NewQueryCursor()
		defer cursor.Close()
		cursor.SetMaxStartDepth(&row.depth)

		matches := cursor.Matches(query, tree.RootNode(), []byte(source))
		assert.Equal(t, row.matches, collectMatches(matches, query, source))
	}
}

func TestQueryErrorDoesNotOob(t *testing.T) {
	language := getLanguage("javascript")

	_, err := NewQuery(language, "(clas")
	assert.Equal(
		t,
		&QueryError{Row: 0, Offset: 1, Column: 1, Kind: QueryErrorNodeType, Message: "clas"},
		err,
	)
}

func TestConsecutiveZeroOrModifiers(t *testing.T) {
	language := getLanguage("javascript")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	zeroSource := ""
	threeSource := "/**/ /**/ /**/"

	zeroTree := parser.Parse([]byte(zeroSource), nil)
	threeTree := parser.Parse([]byte(threeSource), nil)

	tests := []string{
		"(comment)*** @capture",
		"(comment)??? @capture",
		"(comment)*?* @capture",
		"(comment)?*? @capture",
	}

	for _, test := range tests {
		query, err := NewQuery(language, test)
		assert.Nil(t, err)
		defer query.Close()

		cursor := NewQueryCursor()
		defer cursor.Close()
		matches := cursor.Matches(query, zeroTree.RootNode(), []byte(zeroSource))
		assert.NotNil(t, matches.Next())

		matches = cursor.Matches(query, threeTree.RootNode(), []byte(threeSource))

		len3 := false
		len1 := false

		for match := matches.Next(); match != nil; match = matches.Next() {
			if len(match.Captures) == 3 {
				len3 = true
			}
			if len(match.Captures) == 1 {
				len1 = true
			}
		}

		assert.Equal(t, strings.Contains(test, "*"), len3)
		assert.Equal(t, strings.Contains(test, "???"), len1)
	}
}

func TestQueryMaxStartDepthMore(t *testing.T) {
	type row struct {
		depth   uint
		matches []formattedMatch
	}

	source := strings.TrimSpace(`
{
    { }
    {
        { }
    }
}
	`)

	rows := []row{
		{
			depth: 0,
			matches: []formattedMatch{
				fmtMatch(0, fmtCapture("capture", "{\n    { }\n    {\n        { }\n    }\n}")),
			},
		},
		{
			depth: 1,
			matches: []formattedMatch{
				fmtMatch(0, fmtCapture("capture", "{\n    { }\n    {\n        { }\n    }\n}")),
				fmtMatch(0, fmtCapture("capture", "{ }")),
				fmtMatch(0, fmtCapture("capture", "{\n        { }\n    }")),
			},
		},
		{
			depth: 2,
			matches: []formattedMatch{
				fmtMatch(0, fmtCapture("capture", "{\n    { }\n    {\n        { }\n    }\n}")),
				fmtMatch(0, fmtCapture("capture", "{ }")),
				fmtMatch(0, fmtCapture("capture", "{\n        { }\n    }")),
				fmtMatch(0, fmtCapture("capture", "{ }")),
			},
		},
	}

	language := getLanguage("c")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()
	query, err := NewQuery(language, "(compound_statement) @capture")
	assert.Nil(t, err)
	defer query.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	node := matches.Next().Captures[0].Node
	assert.Equal(t, "compound_statement", node.Kind())

	for _, row := range rows {
		cursor.SetMaxStartDepth(&row.depth)

		matches := cursor.Matches(query, &node, []byte(source))
		assert.Equal(t, row.matches, collectMatches(matches, query, source))
	}
}

func TestQueryWithFirstChildInGroupIsAnchor(t *testing.T) {
	language := getLanguage("c")
	sourceCode := "void fun(int a, char b, int c) { };"
	queryStr := `
		(parameter_list
		  .
		  ((parameter_declaration) @constant
			(#match? @constant "^int")))`
	query, err := NewQuery(language, queryStr)
	assert.Nil(t, err)
	defer query.Close()

	assertQueryMatches(
		t,
		language,
		query,
		sourceCode,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("constant", "int a")),
		},
	)
}

func TestQueryWildcardWithImmediateFirstChild(t *testing.T) {
	language := getLanguage("javascript")
	query, err := NewQuery(language, "(_ . (identifier) @firstChild)")
	assert.Nil(t, err)
	defer query.Close()

	source := "function name(one, two, three) { }"

	assertQueryMatches(
		t,
		language,
		query,
		source,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("firstChild", "name")),
			fmtMatch(0, fmtCapture("firstChild", "one")),
		},
	)
}

func TestQueryOnEmptySourceCode(t *testing.T) {
	language := getLanguage("javascript")
	sourceCode := ""
	queryStr := `(program) @program`
	query, err := NewQuery(language, queryStr)
	assert.Nil(t, err)
	defer query.Close()

	assertQueryMatches(
		t,
		language,
		query,
		sourceCode,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("program", "")),
		},
	)
}

func TestQueryExecutionWithTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to millisecond timer resolution limitations")
	}

	language := getLanguage("javascript")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	sourceCode := strings.Repeat("function foo() { while (true) { } }\n", 1000)
	tree := parser.Parse([]byte(sourceCode), nil)
	defer tree.Close()

	query, err := NewQuery(language, "(function_declaration) @function")
	assert.Nil(t, err)
	defer query.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	startTime := time.Now()
	matches := cursor.MatchesWithOptions(
		query,
		tree.RootNode(),
		[]byte(sourceCode),
		QueryCursorOptions{ProgressCallback: func(s QueryCursorState) bool {
			return time.Since(startTime).Microseconds() > 100
		}},
	)
	count := 0
	for matches.Next() != nil {
		count++
	}
	assert.True(t, count < 1000)

	matches = cursor.Matches(query, tree.RootNode(), []byte(sourceCode))
	count = 0
	for matches.Next() != nil {
		count++
	}
	assert.Equal(t, 1000, count)
}

func TestQueryExecutionWithPointsCausingUnderflow(t *testing.T) {
	language := getLanguage("rust")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	sourceCode := `
fn main() {
	println!("{:?}", foo());
}
	`
	parser.SetIncludedRanges([]Range{
		{
			StartByte:  24,
			EndByte:    39,
			StartPoint: Point{0, 0},
			EndPoint:   Point{0, 0},
		},
	})
	tree := parser.Parse([]byte(sourceCode), nil)
	defer tree.Close()

	query, err := NewQuery(language, "(call_expression) @cap")
	assert.Nil(t, err)
	defer query.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(sourceCode))
	assert.Equal(t, 1, len(collectMatches(matches, query, sourceCode)))

	tree.Edit(&InputEdit{
		StartByte:      40,
		OldEndByte:     40,
		NewEndByte:     41,
		StartPosition:  Point{1, 28},
		OldEndPosition: Point{1, 28},
		NewEndPosition: Point{2, 0},
	})

	tree2 := parser.Parse([]byte(sourceCode), tree)
	defer tree2.Close()

	matches2 := cursor.Matches(query, tree2.RootNode(), []byte(sourceCode))
	assert.Equal(t, 1, len(collectMatches(matches2, query, sourceCode)))
}

func TestQueryWithWildcardBehaviorBeforeAnchor(t *testing.T) {
	language := getLanguage("python")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	source := `
		(a, b)
		(c, d,)
	`

	query, err := NewQuery(language, `(tuple (_) @last . ")" .) @match`)
	assert.Nil(t, err)
	defer query.Close()

	assertQueryMatches(
		t,
		language,
		query,
		source,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("match", "(a, b)"), fmtCapture("last", "b")),
			fmtMatch(0, fmtCapture("match", "(c, d,)"), fmtCapture("last", "d")),
		},
	)

	query, err = NewQuery(language, `(tuple _ @last . ")" .) @match`)
	assert.Nil(t, err)
	defer query.Close()

	assertQueryMatches(
		t,
		language,
		query,
		source,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("match", "(a, b)"), fmtCapture("last", "b")),
			fmtMatch(0, fmtCapture("match", "(c, d,)"), fmtCapture("last", ",")),
		},
	)
}

func TestPatternAlternativesFollowLastChildConstraint(t *testing.T) {
	language := getLanguage("rust")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	source := `
fn f() {
	if a {} // <- should NOT match
	if b {}
}
	`

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	query, err := NewQuery(language, `(block
		[
			(type_cast_expression)
			(expression_statement)
		] @last
		.
	)`)
	assert.Nil(t, err)
	defer query.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := collectMatches(cursor.Matches(query, tree.RootNode(), []byte(source)), query, source)

	flippedQuery, err := NewQuery(language, `(block
		[
			(expression_statement)
			(type_cast_expression)
		] @last
		.
	)`)
	assert.Nil(t, err)
	defer flippedQuery.Close()

	flippedMatches := collectMatches(cursor.Matches(flippedQuery, tree.RootNode(), []byte(source)), flippedQuery, source)

	assert.Equal(t, []formattedMatch{fmtMatch(0, fmtCapture("last", "if b {}"))}, matches)
	assert.Equal(t, matches, flippedMatches)
}

func TestWildcardParentAllowsFallibleChildPatterns(t *testing.T) {
	language := getLanguage("javascript")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	sourceCode := `
function foo() {
    "bar"
}
	`

	query, err := NewQuery(language, `(function_declaration
		(_
			(expression_statement)
		)
	) @part`)
	assert.Nil(t, err)
	defer query.Close()

	assertQueryMatches(
		t,
		language,
		query,
		sourceCode,
		[]formattedMatch{
			fmtMatch(0, fmtCapture("part", "function foo() {\n    \"bar\"\n}")),
		},
	)
}

func TestUnfinishedCapturesAreNotDefiniteWithPendingAnchors(t *testing.T) {
	language := getLanguage("javascript")
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	sourceCode := `
const foo = [
  1, 2, 3
]
	`

	tree := parser.Parse([]byte(sourceCode), nil)
	defer tree.Close()

	query, err := NewQuery(language, `(array (_) @foo . "]")`)
	assert.Nil(t, err)
	defer query.Close()

	capturesCursor := NewQueryCursor()
	defer capturesCursor.Close()
	captures := collectCaptures(capturesCursor.Captures(query, tree.RootNode(), []byte(sourceCode)), query, sourceCode)

	matchesCursor := NewQueryCursor()
	defer matchesCursor.Close()
	matches := collectMatches(matchesCursor.Matches(query, tree.RootNode(), []byte(sourceCode)), query, sourceCode)

	assert.Equal(t, []formattedCapture{{"foo", "3"}}, captures)
	assert.Equal(t, 1, len(matches))
	assert.Equal(t, []formattedCapture{{"foo", "3"}}, matches[0].Captures)
}

type formattedCapture struct {
	Name  string
	Value string
}

type formattedMatch struct {
	Index    uint
	Captures []formattedCapture
}

func assertQueryMatches(
	t *testing.T,
	language *Language,
	query *Query,
	source string,
	expected []formattedMatch,
) {
	parser := NewParser()
	defer parser.Close()
	parser.SetLanguage(language)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	cursor := NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), []byte(source))
	assert.Equal(t, expected, collectMatches(matches, query, source))
	assert.False(t, cursor.DidExceedMatchLimit())
}

func collectMatches(
	matches QueryMatches,
	query *Query,
	source string,
) []formattedMatch {
	result := make([]formattedMatch, 0)

	for match := matches.Next(); match != nil; match = matches.Next() {
		result = append(result, fmtMatch(match.PatternIndex, formatCaptures(match.Captures, query, source)...))
	}

	return result
}

func collectCaptures(
	captures QueryCaptures,
	query *Query,
	source string,
) []formattedCapture {
	result := make([]QueryCapture, 0)

	for match, index := captures.Next(); match != nil; match, index = captures.Next() {
		result = append(result, match.Captures[index])
	}

	return formatCaptures(result, query, source)
}

func formatCaptures(
	captures []QueryCapture,
	query *Query,
	source string,
) []formattedCapture {
	result := make([]formattedCapture, 0)

	for _, capture := range captures {
		result = append(result, struct {
			Name  string
			Value string
		}{
			query.CaptureNames()[capture.Index],
			capture.Node.Utf8Text([]byte(source)),
		})
	}

	return result
}

func fmtMatch(
	index uint,
	captures ...formattedCapture,
) formattedMatch {
	if captures == nil {
		captures = make([]formattedCapture, 0)
	}
	return formattedMatch{index, captures}
}

func fmtCapture(name, value string) formattedCapture {
	return formattedCapture{name, value}
}
