package main

import (
	"fmt"
	"go/ast"
	"go/token"
)

const (
	maxFuncLines  = 120
	maxComplexity = 30
)

func checkComplexity(s *sourceFile) []Finding {
	if s.isTest() {
		return nil
	}
	var out []Finding
	for _, decl := range s.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		line := s.line(fn.Pos())
		if n := s.line(fn.Body.Rbrace) - s.line(fn.Body.Lbrace); n > maxFuncLines {
			out = append(out, Finding{s.rel, line, ruleFuncSize,
				fmt.Sprintf("%s is %d lines, over the %d-line limit", funcName(fn), n, maxFuncLines), n - maxFuncLines})
		}
		if c := cyclomatic(fn.Body); c > maxComplexity {
			out = append(out, Finding{s.rel, line, ruleComplexity,
				fmt.Sprintf("%s has cyclomatic complexity %d, over %d", funcName(fn), c, maxComplexity), c - maxComplexity})
		}
	}
	return out
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return receiverName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

// One plus every branch point, the standard cyclomatic count. A bare `default`
// adds no branch, so only case clauses that actually match are counted.
func cyclomatic(body *ast.BlockStmt) int {
	count := 1
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			count++
		case *ast.CaseClause:
			if len(node.List) > 0 {
				count++
			}
		case *ast.CommClause:
			if node.Comm != nil {
				count++
			}
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				count++
			}
		}
		return true
	})
	return count
}
