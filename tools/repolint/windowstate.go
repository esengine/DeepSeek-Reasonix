package main

import (
	"fmt"
	"go/ast"
	"strings"
)

const ruleWindowState = "window-state"

// windowStatePackage is where the context window lives inside the agent.
const windowStatePackage = "internal/runtime/agent/"

// windowStateOwners are the receivers that may touch the window's state
// directly. The loop reaches the window through a.window() and the windowHost
// interface; anything else reading sess.win is a second owner of its locks and
// invariants.
var windowStateOwners = map[string]bool{
	"contextWindow":  true,
	"windowState":    true,
	"sessionRuntime": true, // reset hands a new conversation to the window
	"ContextManager": true, // the window's maintenance transaction
}

// checkWindowState reports sess.win reads outside the window's own methods.
func checkWindowState(s *sourceFile) []Finding {
	rel := strings.ReplaceAll(s.rel, "\\", "/")
	if !strings.HasPrefix(rel, windowStatePackage) || strings.HasSuffix(rel, "_test.go") ||
		strings.Contains(strings.TrimPrefix(rel, windowStatePackage), "/") {
		return nil
	}
	var out []Finding
	for _, decl := range s.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || windowStateOwners[receiverTypeName(fn)] {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "win" {
				return true
			}
			if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "sess" {
				out = append(out, Finding{
					File: s.rel,
					Line: s.fset.Position(sel.Pos()).Line,
					Rule: ruleWindowState,
					Msg: fmt.Sprintf("%s reads the context window's state directly; call it through a.window() "+
						"and let a method on *contextWindow touch sess.win (owners: tools/repolint/windowstate.go)", fn.Name.Name),
					Weight: 1,
				})
			}
			return true
		})
	}
	return out
}

func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
