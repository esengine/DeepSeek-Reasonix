package control

// The driving port promises segregation: a frontend handed sub-ports it never
// calls reaches surfaces port.go says it must not see, so over-declaring fails.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// slackSubPorts is how far a frontend may over-declare. One is the room a
// migration legitimately sits in; eight is a different architecture.
const slackSubPorts = 1

type portModel struct {
	methodOwner map[string]string   // method name → sub-port that declares it
	subPort     map[string]bool     // interfaces that declare methods of their own
	composed    map[string][]string // composite interface → sub-ports it embeds
}

// readPortModel reads the sub-ports and their compositions out of this package.
func readPortModel(t *testing.T) portModel {
	t.Helper()
	model := portModel{methodOwner: map[string]string{}, subPort: map[string]bool{}, composed: map[string][]string{}}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			iface, ok := spec.Type.(*ast.InterfaceType)
			if !ok {
				return true
			}
			for _, field := range iface.Methods.List {
				if len(field.Names) == 1 {
					model.methodOwner[field.Names[0].Name] = spec.Name.Name
					model.subPort[spec.Name.Name] = true
					continue
				}
				if embedded, ok := field.Type.(*ast.Ident); ok {
					model.composed[spec.Name.Name] = append(model.composed[spec.Name.Name], embedded.Name)
				}
			}
			return true
		})
	}
	return model
}

// portsOf expands a composite to the sub-ports it makes visible; a sub-port
// named directly expands to itself.
func (m portModel) portsOf(name string) map[string]bool {
	out := map[string]bool{}
	if members, ok := m.composed[name]; ok {
		for _, member := range members {
			for p := range m.portsOf(member) {
				out[p] = true
			}
		}
		return out
	}
	out[name] = true
	return out
}

// frontendUse walks a frontend's non-test sources for the port types it names
// and the port methods it actually calls.
func frontendUse(t *testing.T, dir string, model portModel) (declared, called map[string]bool) {
	t.Helper()
	declared, called = map[string]bool{}, map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "control" {
				// Only a port type counts. Everything else this package exports —
				// constants, helpers, the concrete Controller — is not a port and
				// says nothing about which surface a frontend drives.
				if _, composite := model.composed[sel.Sel.Name]; composite || model.subPort[sel.Sel.Name] {
					for p := range model.portsOf(sel.Sel.Name) {
						declared[p] = true
					}
				}
			}
			if owner, ok := model.methodOwner[sel.Sel.Name]; ok {
				called[owner] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return declared, called
}

func TestFrontendsDeclareOnlyThePortsTheyDrive(t *testing.T) {
	model := readPortModel(t)
	if len(model.methodOwner) == 0 || len(model.composed) == 0 {
		t.Fatal("no sub-ports read from this package; this guard is watching nothing")
	}
	// cli is absent: with the interactive session gone it launches serve and a
	// one-shot run rather than operating a session, so a sub-port it never calls
	// is not a scope it claimed too widely.
	for _, frontend := range []string{"acp", "serve"} {
		t.Run(frontend, func(t *testing.T) {
			dir := filepath.Join("..", frontend)
			if _, err := os.Stat(dir); err != nil {
				t.Skipf("no %s package: %v", frontend, err)
			}
			declared, called := frontendUse(t, dir, model)
			if len(declared) == 0 {
				t.Skip("this frontend names no port type")
			}
			var spare []string
			for port := range declared {
				if !called[port] {
					spare = append(spare, port)
				}
			}
			slices.Sort(spare)
			if len(spare) > slackSubPorts {
				t.Errorf("%s is handed %d sub-ports it never calls: %s\n"+
					"Name the composite it actually drives (see EditorAPI), or add the calls.",
					frontend, len(spare), strings.Join(spare, ", "))
			}
		})
	}
}
