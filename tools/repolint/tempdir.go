package main

import (
	"go/ast"
	"strconv"
	"strings"
)

const ruleTempDir = "temp-dir"

// checkTempDir reports a test that takes its directory from the standard
// TempDir, whose cleanup fails on Windows while a deleted file's handle is still
// open; testenv.TempDir retries. An os-qualified call is the system's temp path.
func checkTempDir(s *sourceFile) []Finding {
	rel := strings.ReplaceAll(s.rel, "\\", "/")
	if !strings.HasSuffix(rel, "_test.go") || under(rel, "internal/base/testenv") {
		return nil
	}
	osName := ""
	for _, imp := range s.file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == "os" {
			osName = "os"
			if imp.Name != nil {
				osName = imp.Name.Name
			}
		}
	}
	var out []Finding
	ast.Inspect(s.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "TempDir" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && osName != "" && id.Name == osName {
			return true
		}
		out = append(out, Finding{
			File: s.rel,
			Line: s.fset.Position(call.Pos()).Line,
			Rule: ruleTempDir,
			Msg: "use testenv.TempDir(t): the standard cleanup fails on Windows while a deleted file's " +
				"handle is still open, and testenv's retries (tools/repolint/tempdir.go)",
			Weight: 1,
		})
		return true
	})
	return out
}
