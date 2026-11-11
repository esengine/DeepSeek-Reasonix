// Package proctest holds the check that keeps an owner's mapping onto the
// process vocabulary total.
package proctest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/contract/process"
)

// Declared returns the string values of every constant of typeName declared in
// the non-test files of dir.
func Declared(t testing.TB, dir, typeName string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				id, ok := vs.Type.(*ast.Ident)
				if !ok || id.Name != typeName || len(vs.Values) != 1 {
					continue
				}
				if lit, ok := vs.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					v, _ := strconv.Unquote(lit.Value)
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// Total fails unless every declared value of typeName maps to a valid state.
func Total(t testing.TB, dir, typeName string, class func(string) (process.Class, bool)) {
	t.Helper()
	vals := Declared(t, dir, typeName)
	if len(vals) == 0 {
		t.Fatalf("no constants of %s found in %s", typeName, dir)
	}
	for _, v := range vals {
		c, ok := class(v)
		if !ok || !c.State.Valid() {
			t.Errorf("%s %q has no mapping onto the process vocabulary", typeName, v)
		}
	}
}
