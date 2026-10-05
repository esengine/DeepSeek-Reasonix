package storage

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/instruction"
)

// stateRootExemptions are top-level names joined under the state root that a
// relocation deliberately leaves alone, each with the reason.
var stateRootExemptions = map[string]string{
	"worktrees":             "its own root; checkouts hold absolute links a copy would break",
	".env":                  "credentials stay in the home root, which the loader reads first and a relocation never moves",
	"credentials":           "credentials stay in the home root, which the loader reads first and a relocation never moves",
	"desktop-window.json":   "derived state of the retired desktop shell; only the repair reset touches it",
	"desktop-zoom.json":     "derived state of the retired desktop shell; only the repair reset touches it",
	"desktop-tabs.json":     "derived state of the retired desktop shell; only the repair reset touches it",
	"desktop-projects.json": "derived state of the retired desktop shell; only the repair reset touches it",
}

var stateRootMarkers = []string{"MemoryUserDir()", "userSupportDir()", "Dir(RootState)", "RootDir(config.RootState)"}
var stateRootNames = []string{"userDir", "UserDir", "stateRoot", "memoryUserDir"}

// A top-level name joined under the state root and absent from StateRootEntries
// is left behind by Settings > Storage while the code reads only the new root.
func TestEveryTopLevelStatePathIsAnOwnedEntry(t *testing.T) {
	root := repoInternalDir(t)
	consts := stringConsts(t, root)
	found := map[string]string{}
	fset := token.NewFileSet()
	walkGo(t, root, func(path string) {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			scanFunc(fset, filepath.Dir(path), fn, consts, func(name string) { found[name] = path })
		}
	})
	if len(found) == 0 {
		t.Fatal("scan found no state-root joins; the guard is not looking at anything")
	}
	var missing []string
	for name, path := range found {
		if slices.Contains(config.StateRootEntries, name) || stateRootExemptions[name] != "" {
			continue
		}
		missing = append(missing, name+" ("+filepath.ToSlash(strings.TrimPrefix(path, root))+")")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("top-level state paths not in config.StateRootEntries (a move would strand them): %v", missing)
	}
}

func TestInstructionDocumentsAreOwnedEntries(t *testing.T) {
	for _, name := range append(slices.Clone(instruction.DocumentNames), instruction.LocalDocumentNames...) {
		if !slices.Contains(config.StateRootEntries, name) {
			t.Errorf("%s is read from the state root but is not in StateRootEntries", name)
		}
	}
}

func scanFunc(fset *token.FileSet, dir string, fn *ast.FuncDecl, consts map[string]string, emit func(string)) {
	derived := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i, lhs := range assign.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && isStateExpr(nodeText(fset, assign.Rhs[i]), nil) {
				derived[id.Name] = true
			}
		}
		return true
	})
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 || !isJoin(call.Fun) {
			return true
		}
		if !isStateExpr(nodeText(fset, call.Args[0]), derived) {
			return true
		}
		if name, ok := literal(call.Args[1], scoped(consts, dir)); ok && name != "" {
			emit(name)
		}
		return true
	})
}

func isJoin(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name == "Join"
	case *ast.Ident:
		return f.Name == "joinRoot"
	}
	return false
}

func isStateExpr(text string, derived map[string]bool) bool {
	for _, m := range stateRootMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	if derived[text] {
		return true
	}
	return slices.ContainsFunc(stateRootNames, func(n string) bool { return text == n || strings.HasSuffix(text, "."+n) })
}

func literal(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			s, err := strconv.Unquote(v.Value)
			return s, err == nil
		}
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.SelectorExpr:
		s, ok := consts[v.Sel.Name]
		return s, ok
	}
	return "", false
}

func nodeText(fset *token.FileSet, n ast.Node) string {
	var b strings.Builder
	_ = printNode(&b, fset, n)
	return b.String()
}

func stringConsts(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	clash := map[string]bool{}
	fset := token.NewFileSet()
	walkGo(t, root, func(path string) {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if s, ok := literal(vs.Values[i], nil); ok {
							key := filepath.Dir(path) + "|" + name.Name
							out[key] = s
							if prev, seen := out[name.Name]; seen && prev != s {
								clash[name.Name] = true
							}
							out[name.Name] = s
						}
					}
				}
			}
		}
	})
	for name := range clash {
		delete(out, name)
	}
	return out
}

func walkGo(t *testing.T, root string, visit func(path string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			visit(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func repoInternalDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func printNode(b *strings.Builder, fset *token.FileSet, n ast.Node) error {
	return printer.Fprint(b, fset, n)
}

func scoped(consts map[string]string, dir string) map[string]string {
	out := map[string]string{}
	for k, v := range consts {
		if pkg, name, ok := strings.Cut(k, "|"); ok {
			if pkg == dir {
				out[name] = v
			}
			continue
		}
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}
