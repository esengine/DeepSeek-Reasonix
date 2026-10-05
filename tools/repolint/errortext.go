package main

import (
	"go/ast"
	"strings"
)

// Only the producer knows a deadline expired rather than a socket closing, and
// a message is where that knowledge dies: the reader is left matching words
// that drift, get reworded, or arrive in the operating system's language. The
// identity travels instead — a sentinel through errors.Is, a typed code.
var textMatchers = map[string]bool{
	"Contains": true, "HasPrefix": true, "HasSuffix": true,
	"Index": true, "EqualFold": true, "Cut": true, "Split": true,
}

// checkErrorText flags matching on an error's rendered text. It follows the
// text one hop into a local, because storing err.Error() first is the shape the
// direct form turns into once the direct form is gone.
func checkErrorText(s *sourceFile) []Finding {
	if strings.HasSuffix(s.rel, "_test.go") {
		return nil
	}
	var out []Finding
	ast.Inspect(s.file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		out = append(out, errorTextInBody(s, fn.Body)...)
		return true
	})
	return out
}

func errorTextInBody(s *sourceFile, body *ast.BlockStmt) []Finding {
	tainted := map[string]bool{}
	var out []Finding
	ast.Inspect(body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			markErrorText(as, tainted)
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isStringsMatcher(call.Fun) || len(call.Args) == 0 {
			return true
		}
		if !readsErrorText(call.Args[0], tainted) {
			return true
		}
		out = append(out, Finding{
			File:   s.rel,
			Line:   s.fset.Position(call.Pos()).Line,
			Rule:   ruleErrorText,
			Msg:    "matching an error's text; read its identity instead (errors.Is on a sentinel, or a typed code)",
			Weight: 1,
		})
		return true
	})
	return out
}

// markErrorText remembers a local holding an error's rendered text, including
// one wrapped in a case fold — the two spellings of the same mistake.
func markErrorText(as *ast.AssignStmt, tainted map[string]bool) {
	for i, rhs := range as.Rhs {
		if i >= len(as.Lhs) || !yieldsErrorText(rhs, tainted) {
			continue
		}
		if id, ok := as.Lhs[i].(*ast.Ident); ok {
			tainted[id.Name] = true
		}
	}
}

func yieldsErrorText(e ast.Expr, tainted map[string]bool) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Error" && len(call.Args) == 0 {
		return true
	}
	if isSelector(call.Fun, "strings", "ToLower") || isSelector(call.Fun, "strings", "ToUpper") ||
		isSelector(call.Fun, "strings", "TrimSpace") {
		return len(call.Args) == 1 && yieldsErrorText(call.Args[0], tainted)
	}
	return false
}

func readsErrorText(e ast.Expr, tainted map[string]bool) bool {
	if id, ok := e.(*ast.Ident); ok {
		return tainted[id.Name]
	}
	return yieldsErrorText(e, tainted)
}

func isStringsMatcher(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || !textMatchers[sel.Sel.Name] {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "strings"
}
