package main

import (
	"fmt"
	"go/ast"
	"path"
	"strconv"
	"strings"
)

const ruleProcessInput = "process-input"

// processInputReads are the os functions that answer from the process rather
// than from what a caller stated. Inside the kernel a build may serve a home
// or a workspace other than the process's, so each read is a second source of
// an input Build is handed (boot.Options, config.Roots); hosts read them once.
var processInputReads = map[string]bool{
	"Getenv": true, "LookupEnv": true, "Environ": true, "Getwd": true,
	"UserHomeDir": true, "UserConfigDir": true, "UserCacheDir": true,
}

// checkProcessInput reports each reference to a process input in the kernel,
// assembly included; internal/frontend and the hosts are where the process is
// read. A reference counts whether it is called or passed as a value; names are
// not resolved, so a local that shadows the os import counts too.
func checkProcessInput(s *sourceFile) []Finding {
	rel := strings.ReplaceAll(s.rel, "\\", "/")
	pkg := path.Dir(rel)
	if !under(pkg, "internal") || under(pkg, "internal/frontend") || strings.HasSuffix(rel, "_test.go") {
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
	if osName == "" || osName == "_" {
		return nil
	}
	var out []Finding
	ast.Inspect(s.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !processInputReads[sel.Sel.Name] {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == osName {
			out = append(out, Finding{
				File: s.rel,
				Line: s.fset.Position(sel.Pos()).Line,
				Rule: ruleProcessInput,
				Msg: fmt.Sprintf("os.%s reads the process; take it from the inputs the caller states "+
					"(boot.Options, config.Roots) so a build bound elsewhere does not read this process's (tools/repolint/processinput.go)", sel.Sel.Name),
				Weight: 1,
			})
		}
		return true
	})
	return out
}
