package update

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestStudioCatalogNamesTheStudioLine(t *testing.T) {
	if StudioCatalog == "" {
		t.Fatal("StudioCatalog is empty, so every Options built from it fails at fetch")
	}
	if !strings.Contains(StudioCatalog, "/studio/") {
		t.Fatalf("StudioCatalog does not name the studio line (%q); its entries would name another product's artifacts", StudioCatalog)
	}
}

// A release listed by one catalog cannot be installed from another, and an
// Options that omits IndexURL fails only once a user asks for an update. The
// guard reads this package's own source because the failure mode is a new call
// site rather than a changed one; desktop/next holds the same guard over the
// literals written as update.Options there.
func TestEveryOptionsLiteralSetsIndexURL(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	found := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Every file, whatever its build tags: an Options behind a tag reads the
		// wrong catalog just as surely as one in the default build.
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "Options" {
				return true
			}
			found++
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "IndexURL" {
					return true
				}
			}
			t.Errorf("%s: Options does not set IndexURL, so its fetch has no catalog to read", fset.Position(lit.Pos()))
			return true
		})
	}
	if found == 0 {
		t.Fatal("no Options literal found; this guard is no longer watching anything")
	}
}
