package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A directory written under the state root but missing from StateRootEntries is
// left behind by a relocation, while the config that names what was in it moves
// with the rest. The wallpaper, the theme packs and the update rollback backups
// were all in that position. This reads the tree rather than trusting a list
// someone has to remember to extend.
func TestStateRootEntriesCoversEverythingWrittenThere(t *testing.T) {
	found, err := stateRootJoins(filepath.Join("..", "..", "..", "internal"))
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("no state-root joins found; this guard is watching nothing")
	}
	for name, where := range found {
		if !slices.Contains(StateRootEntries, name) {
			t.Errorf("%q is written under the state root (%s) but is not in StateRootEntries, so a relocation leaves it behind", name, where)
		}
	}
}

// stateRootJoins maps each literal name joined onto the state root to a file
// doing it. An entry another process removes mid-walk is not a source file.
func stateRootJoins(dir string) (map[string]string, error) {
	found := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil && vanishedMidWalk(path, err) {
			return nil
		}
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isJoin(call.Fun) || len(call.Args) < 2 || !isStateRootCall(call.Args[0]) {
				return true
			}
			if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if name, err := strconv.Unquote(lit.Value); err == nil {
					found[name] = path
				}
			}
			return true
		})
		return nil
	})
	return found, err
}

func isJoin(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Join" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "filepath"
}

// The state root is reached through either name; both resolve to the same dir.
func isStateRootCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == "userSupportDir" || fun.Name == "MemoryUserDir"
	case *ast.SelectorExpr:
		return fun.Sel.Name == "MemoryUserDir"
	}
	return false
}
