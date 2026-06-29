package control

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestComposedSyntheticRunsCarryMemoryCompilerSkip(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var composeCalls int
	var coveredRunCalls int
	var failures []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isSelector(call.Fun, "ComposeSynthetic") {
				composeCalls++
			}
			if !isSelector(call.Fun, "Run") || !callsSelector(call, "ComposeSynthetic") {
				return true
			}
			coveredRunCalls++
			if len(call.Args) == 0 || !callsSelector(call.Args[0], "WithMemoryCompilerSkip") {
				failures = append(failures, fset.Position(call.Pos()).String())
			}
			return true
		})
	}

	if len(failures) > 0 {
		t.Fatalf("ComposeSynthetic runner calls without MemoryCompilerSkip at: %s", strings.Join(failures, ", "))
	}
	if composeCalls != coveredRunCalls {
		t.Fatalf("ComposeSynthetic calls = %d, but only %d are direct Run calls with contract checks", composeCalls, coveredRunCalls)
	}
	if composeCalls == 0 {
		t.Fatal("test did not find any ComposeSynthetic call in non-test control code")
	}
}

func callsSelector(n ast.Node, selector string) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isSelector(call.Fun, selector) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isSelector(expr ast.Expr, selector string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == selector
}
