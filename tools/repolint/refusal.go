package main

import (
	"go/ast"
	"go/token"
	"strings"
)

// A refusal the client can act on carries a stable dotted code: the frontend
// looks the code up to say it in the reader's language, and falls back to the
// English only when it has no wording. http.Error writes prose with no code, so
// a caller sees a status and a sentence nobody translated — and, before the
// transport read plain bodies at all, only the status.
const (
	refusalPackage = "internal/frontend/serve/"
	refusalAdapter = "internal/frontend/serve/fail.go"
)

// checkRefusalPath flags a refusal that carries no code where the coded one
// belongs: http.Error, and a JSON body that is nothing but a message.
func checkRefusalPath(s *sourceFile) []Finding {
	if !strings.HasPrefix(s.rel, refusalPackage) || strings.HasSuffix(s.rel, "_test.go") {
		return nil
	}
	// writeErr is the one adapter allowed plain text: it renders a coded error
	// and falls back for one carrying none, which is what lets the code below
	// adopt codes without a flag day. The exemption is this file, not its name.
	if s.rel == refusalAdapter {
		return nil
	}
	var out []Finding
	flag := func(n ast.Node, msg string) {
		out = append(out, Finding{File: s.rel, Line: s.fset.Position(n.Pos()).Line, Rule: ruleRefusalPath, Msg: msg, Weight: 1})
	}
	ast.Inspect(s.file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if isSelector(node.Fun, "http", "Error") {
				flag(node, "http.Error sends a refusal with no code; use refuse() so the frontend can say it")
			}
		case *ast.CompositeLit:
			if errorOnlyMap(node) {
				flag(node, `a body of only {"error": ...} refuses with no code; use refuse() or saveFailed() so the frontend can say it`)
			}
		}
		return true
	})
	return out
}

// errorOnlyMap reports a JSON body that is nothing but a message. It is the
// same refusal http.Error sends wearing a different envelope, and the reader
// ends up in the same place: an English sentence and a status. A body that
// carries other fields is a report the frontend renders, not this.
func errorOnlyMap(lit *ast.CompositeLit) bool {
	mapType, ok := lit.Type.(*ast.MapType)
	if !ok || len(lit.Elts) != 1 {
		return false
	}
	if key, ok := mapType.Key.(*ast.Ident); !ok || key.Name != "string" {
		return false
	}
	entry, ok := lit.Elts[0].(*ast.KeyValueExpr)
	if !ok {
		return false
	}
	name, ok := entry.Key.(*ast.BasicLit)
	return ok && name.Kind == token.STRING && name.Value == `"error"`
}

func isSelector(fun ast.Expr, pkg, name string) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}
