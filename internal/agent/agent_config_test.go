package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configFieldNames is agentConfig's surface, read from the type itself so the
// guard below cannot drift from what the struct actually holds.
func configFieldNames(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "agent_config.go", nil, 0)
	if err != nil {
		t.Fatalf("parse agent_config.go: %v", err)
	}
	names := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "agentConfig" {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				names[name.Name] = true
			}
		}
		return false
	})
	if len(names) == 0 {
		t.Fatal("agentConfig has no fields; the guard would pass vacuously")
	}
	return names
}

// Field promotion makes `a.contextWindow = x` compile from anywhere in the
// package, so "configuration" is a claim rather than a guarantee. That claim is
// what lets the struct-state ratchet exclude these fields; unenforced, the
// exclusion would just be a way to hide state. So it is checked, not asserted.
func TestAgentConfigIsNeverAssignedAfterConstruction(t *testing.T) {
	fields := configFieldNames(t)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || !fields[sel.Sel.Name] {
					continue
				}
				// Only Agent's own receiver: other types (TaskTool) legitimately
				// carry same-named fields of their own.
				recv, ok := sel.X.(*ast.Ident)
				if !ok || recv.Name != "a" {
					continue
				}
				t.Errorf("%s:%d: %s.%s is assigned after construction; agentConfig must stay immutable, or the field belongs on Agent",
					name, fset.Position(sel.Pos()).Line, recv.Name, sel.Sel.Name)
			}
			return true
		})
	}
}
