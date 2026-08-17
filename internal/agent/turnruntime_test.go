package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// beginRunTurn opens with `a.turn = turnRuntime{}`, so a new field starts the
// next turn zeroed. That holds only while the type stays assignable: one mutex
// or atomic makes it a vet copylocks error, and the lifetime degrades to
// whatever the call sites happen to reset. sessionRuntime already pays that
// price; this layer must not.
func TestTurnRuntimeStaysAssignable(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "turnruntime.go", nil, 0)
	if err != nil {
		t.Fatalf("parse turnruntime.go: %v", err)
	}
	var fields int
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "turnRuntime" {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			fields += max(len(field.Names), 1)
			sel, ok := unwrapStar(field.Type).(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				continue
			}
			switch qualified := pkg.Name + "." + sel.Sel.Name; {
			case pkg.Name == "atomic", qualified == "sync.Mutex", qualified == "sync.RWMutex":
				t.Errorf("turnRuntime.%s is a %s; beginRunTurn's single assignment stops compiling and the reset guarantee is lost",
					fieldName(field), qualified)
			}
		}
		return false
	})
	if fields == 0 {
		t.Fatal("turnRuntime has no fields; the guard would pass vacuously")
	}
}

func unwrapStar(expr ast.Expr) ast.Expr {
	if star, ok := expr.(*ast.StarExpr); ok {
		return star.X
	}
	return expr
}

func fieldName(field *ast.Field) string {
	if len(field.Names) == 0 {
		return "<embedded>"
	}
	return field.Names[0].Name
}

// The tool path must reach turn state through its parameter. Reading a.turn
// inside it would compile and behave identically today — and would silently
// stop being true the moment a turn is not the agent's current one, which is
// exactly what the parameter exists to prevent.
func TestToolPathTakesTheTurnAsAParameter(t *testing.T) {
	for _, name := range []string{"execute_one.go", "execute_batch.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "turn" {
				return true
			}
			if recv, ok := sel.X.(*ast.Ident); ok && recv.Name == "a" {
				t.Errorf("%s:%d: reads a.turn; the tool path takes *turnRuntime as a parameter",
					name, fset.Position(sel.Pos()).Line)
			}
			return true
		})
	}
}

func TestBeginRunTurnReplacesTheWholeTurn(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "run_loop.go", nil, 0)
	if err != nil {
		t.Fatalf("parse run_loop.go: %v", err)
	}
	var replaced bool
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "beginRunTurn" {
			return true
		}
		ast.Inspect(fn, func(inner ast.Node) bool {
			assign, ok := inner.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			sel, ok := assign.Lhs[0].(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "turn" {
				return true
			}
			if lit, ok := assign.Rhs[0].(*ast.CompositeLit); ok {
				if ident, ok := lit.Type.(*ast.Ident); ok && ident.Name == "turnRuntime" {
					if len(lit.Elts) != 0 {
						t.Error("beginRunTurn seeds fields in the replacement literal; keep it empty so nothing is carried by accident")
					}
					replaced = true
				}
			}
			return true
		})
		return false
	})
	if !replaced {
		t.Error("beginRunTurn no longer replaces a.turn wholesale; a per-field reset can forget a field")
	}
}
