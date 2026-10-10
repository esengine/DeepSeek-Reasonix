package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionAgentOptionsCarryTargetAccess(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range []string{"cmd", "internal", "desktop"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "node_modules", "testdata", "vendor", ".git":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			local := filepath.Dir(path) == filepath.Join(root, "internal", "runtime", "agent")
			count, missing := targetAccessWiring(file, local)
			checked += count
			for _, pos := range missing {
				t.Errorf("%s: production agent.Options must initialize CheckTargetAccess", fset.Position(pos))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no production agent.Options were inspected")
	}
}

func targetAccessWiring(file *ast.File, local bool) (int, []token.Pos) {
	aliases := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err == nil && path == "reasonix/internal/runtime/agent" {
			name := "agent"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			aliases[name] = true
		}
	}
	isOptions := func(expr ast.Expr) bool {
		if selector, ok := expr.(*ast.SelectorExpr); ok {
			pkg, ok := selector.X.(*ast.Ident)
			return ok && aliases[pkg.Name] && selector.Sel.Name == "Options"
		}
		ident, ok := expr.(*ast.Ident)
		return ok && (local || aliases["."]) && ident.Name == "Options"
	}
	count := 0
	var missing []token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CompositeLit:
			if isOptions(node.Type) {
				count++
				if !hasTargetAccessField(node) {
					missing = append(missing, node.Pos())
				}
			}
		case *ast.ValueSpec:
			if isOptions(node.Type) && len(node.Values) == 0 {
				count++
				missing = append(missing, node.Pos())
			}
		}
		return true
	})
	return count, missing
}

func hasTargetAccessField(lit *ast.CompositeLit) bool {
	for _, element := range lit.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if ok && key.Name == "CheckTargetAccess" {
			value, nilValue := field.Value.(*ast.Ident)
			return !nilValue || value.Name != "nil"
		}
	}
	return false
}

func TestTargetAccessWiringDetectsMissingChecks(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		local        bool
		missing      int
	}{
		{"wired", `package p; import "reasonix/internal/runtime/agent"; var opts = agent.Options{CheckTargetAccess: check}`, false, 0},
		{"missing", `package p; import "reasonix/internal/runtime/agent"; var opts = agent.Options{}`, false, 1},
		{"nil", `package p; import "reasonix/internal/runtime/agent"; var opts = agent.Options{CheckTargetAccess: nil}`, false, 1},
		{"alias", `package p; import runtimeagent "reasonix/internal/runtime/agent"; var opts = runtimeagent.Options{}`, false, 1},
		{"zero", `package p; import "reasonix/internal/runtime/agent"; var opts agent.Options`, false, 1},
		{"local", `package agent; var opts = Options{}`, true, 1},
		{"other", `package p; import "other/agent"; var opts = agent.Options{}`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, missing := targetAccessWiring(file, tc.local)
			if len(missing) != tc.missing {
				t.Fatalf("missing checks = %d, want %d", len(missing), tc.missing)
			}
		})
	}
}
