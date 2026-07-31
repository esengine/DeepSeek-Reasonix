// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax_test

import (
	"fmt"
	"os"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func Example() {
	r := strings.NewReader("{ foo; bar; }")
	f, err := syntax.NewParser().Parse(r, "")
	if err != nil {
		return
	}
	syntax.NewPrinter().Print(os.Stdout, f)
	// Output:
	// {
	//	foo
	//	bar
	// }
}

func ExampleWord() {
	r := strings.NewReader("echo foo${bar}'baz'")
	f, err := syntax.NewParser().Parse(r, "")
	if err != nil {
		return
	}

	printer := syntax.NewPrinter()
	args := f.Stmts[0].Cmd.(*syntax.CallExpr).Args
	for i, word := range args {
		fmt.Printf("Word number %d:\n", i)
		for _, part := range word.Parts {
			fmt.Printf("%-20T - ", part)
			printer.Print(os.Stdout, part)
			fmt.Println()
		}
		fmt.Println()
	}

	// Output:
	// Word number 0:
	// *syntax.Lit          - echo
	//
	// Word number 1:
	// *syntax.Lit          - foo
	// *syntax.ParamExp     - ${bar}
	// *syntax.SglQuoted    - 'baz'
}

func ExampleCommand() {
	r := strings.NewReader("echo foo; if x; then y; fi; foo | bar")
	f, err := syntax.NewParser().Parse(r, "")
	if err != nil {
		return
	}

	printer := syntax.NewPrinter()
	for i, stmt := range f.Stmts {
		fmt.Printf("Cmd %d: %-20T - ", i, stmt.Cmd)
		printer.Print(os.Stdout, stmt.Cmd)
		fmt.Println()
	}

	// Output:
	// Cmd 0: *syntax.CallExpr     - echo foo
	// Cmd 1: *syntax.IfClause     - if x; then y; fi
	// Cmd 2: *syntax.BinaryCmd    - foo | bar
}

func ExampleNewParser_options() {
	src := "for ((i = 0; i < 5; i++)); do echo $i >f; done"

	// LangBash is the default
	r := strings.NewReader(src)
	f, err := syntax.NewParser().Parse(r, "")
	fmt.Println(err)

	// Parser errors with LangPOSIX
	r = strings.NewReader(src)
	_, err = syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(r, "")
	fmt.Println(err)

	syntax.NewPrinter().Print(os.Stdout, f)
	syntax.NewPrinter(syntax.SpaceRedirects(true)).Print(os.Stdout, f)

	// Output:
	// <nil>
	// 1:5: c-style fors are a bash/zsh feature; tried parsing as posix
	// for ((i = 0; i < 5; i++)); do echo $i >f; done
	// for ((i = 0; i < 5; i++)); do echo $i > f; done
}

// Keep in sync with FuzzQuote.

func ExampleQuote() {
	for _, s := range []string{
		"foo",
		"bar $baz",
		`"won't"`,
		"~/home",
		"#1304",
		"name=value",
		"for",
		"glob-*",
		"invalid-\xe2'",
		"nonprint-\x0b\x1b",
	} {
		// We assume Bash syntax here.
		// For general shell syntax quoting, use syntax.LangPOSIX.
		quoted, err := syntax.Quote(s, syntax.LangBash)
		if err != nil {
			fmt.Printf("%q cannot be quoted: %v\n", s, err)
		} else {
			fmt.Printf("Quote(%17q): %s\n", s, quoted)
		}
	}
	// Output:
	// Quote(            "foo"): foo
	// Quote(       "bar $baz"): 'bar $baz'
	// Quote(      "\"won't\""): "\"won't\""
	// Quote(         "~/home"): '~/home'
	// Quote(          "#1304"): '#1304'
	// Quote(     "name=value"): 'name=value'
	// Quote(            "for"): 'for'
	// Quote(         "glob-*"): 'glob-*'
	// Quote(  "invalid-\xe2'"): $'invalid-\xe2\''
	// Quote("nonprint-\v\x1b"): $'nonprint-\v\x1b'
}

func ExampleWalk() {
	in := strings.NewReader(`echo $foo "and $bar"`)
	f, err := syntax.NewParser().Parse(in, "")
	if err != nil {
		return
	}
	syntax.Walk(f, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.ParamExp:
			node.Param.Value = strings.ToUpper(node.Param.Value)
		}
		return true
	})
	syntax.NewPrinter().Print(os.Stdout, f)
	// Output: echo $FOO "and $BAR"
}

func ExampleDebugPrint() {
	in := strings.NewReader(`echo 'foo'`)
	f, err := syntax.NewParser().Parse(in, "")
	if err != nil {
		return
	}
	syntax.DebugPrint(os.Stdout, f)
	// Output:
	// *syntax.File {
	// .  Name: ""
	// .  Stmts: []*syntax.Stmt (len = 1) {
	// .  .  0: *syntax.Stmt {
	// .  .  .  Comments: []syntax.Comment (len = 0) {}
	// .  .  .  Cmd: *syntax.CallExpr {
	// .  .  .  .  Assigns: []*syntax.Assign (len = 0) {}
	// .  .  .  .  Args: []*syntax.Word (len = 2) {
	// .  .  .  .  .  0: *syntax.Word {
	// .  .  .  .  .  .  Parts: []syntax.WordPart (len = 1) {
	// .  .  .  .  .  .  .  0: *syntax.Lit {
	// .  .  .  .  .  .  .  .  ValuePos: 1:1
	// .  .  .  .  .  .  .  .  ValueEnd: 1:5
	// .  .  .  .  .  .  .  .  Value: "echo"
	// .  .  .  .  .  .  .  }
	// .  .  .  .  .  .  }
	// .  .  .  .  .  }
	// .  .  .  .  .  1: *syntax.Word {
	// .  .  .  .  .  .  Parts: []syntax.WordPart (len = 1) {
	// .  .  .  .  .  .  .  0: *syntax.SglQuoted {
	// .  .  .  .  .  .  .  .  Left: 1:6
	// .  .  .  .  .  .  .  .  Right: 1:10
	// .  .  .  .  .  .  .  .  Dollar: false
	// .  .  .  .  .  .  .  .  Value: "foo"
	// .  .  .  .  .  .  .  }
	// .  .  .  .  .  .  }
	// .  .  .  .  .  }
	// .  .  .  .  }
	// .  .  .  }
	// .  .  .  Position: 1:1
	// .  .  .  Semicolon: 0:0
	// .  .  .  Negated: false
	// .  .  .  Background: false
	// .  .  .  Coprocess: false
	// .  .  .  Disown: false
	// .  .  .  Redirs: []*syntax.Redirect (len = 0) {}
	// .  .  }
	// .  }
	// .  Last: []syntax.Comment (len = 0) {}
	// }
}
