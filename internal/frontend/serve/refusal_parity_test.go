package serve

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// codeArg is where each helper carries the dotted code, so a new one is added
// here rather than by teaching a regex another shape.
var codeArg = map[string]int{"refuse": 2, "busy": 1, "coded": 0, "busyErr": 0, "refusal": 1, "Refusal": 1, "saveFailed": 2}

// Where a coded refusal can be built. The exported constructor put the second
// one outside this package, and a guard that only watches its own directory
// would have let the next one through untranslated.
var refusalDirs = []string{".", filepath.Join("..", "..", "..", "desktop", "next")}

// serveRefusalCodes reads the codes this package can send, from the syntax
// rather than from the text: a scan that matched on wording would be the very
// defect the coded refusal exists to end.
func serveRefusalCodes(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	var sources []string
	for _, dir := range refusalDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// The desktop tree is absent in some checkouts; its own package is
			// where that would be a failure, not here.
			continue
		}
		for _, e := range entries {
			if name := e.Name(); !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				sources = append(sources, filepath.Join(dir, name))
			}
		}
	}
	consts := packageStringConsts(t, fset, sources)
	for _, name := range sources {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// A host outside this package calls it qualified, and matching only
			// bare identifiers is how the guard silently stopped watching one.
			fn := ""
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				fn = callee.Name
			case *ast.SelectorExpr:
				fn = callee.Sel.Name
			}
			at, ok := codeArg[fn]
			if !ok || at >= len(call.Args) {
				return true
			}
			code := codeOf(call.Args[at], consts)
			if strings.Contains(code, ".") {
				out[code] = name
			}
			return true
		})
	}
	return out
}

func translatedCodes(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", "..", "desktop", "frontend-next", "src", "i18n", "kernel.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no frontend catalogue: %v", err)
	}
	out := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		key, _, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		if code, err := strconv.Unquote(key); err == nil && strings.Contains(code, ".") {
			out[code] = true
		}
	}
	return out
}

// A code the frontend cannot say degrades to the kernel's English, which no
// reader asked for. This is what kernel.ts exports `codes` for.
func TestEveryRefusalCodeIsTranslated(t *testing.T) {
	said := translatedCodes(t)
	if len(said) == 0 {
		t.Fatal("no codes read from the frontend catalogue; this guard is watching nothing")
	}
	var missing []string
	for code, file := range serveRefusalCodes(t) {
		if !said[code] {
			missing = append(missing, code+" ("+file+")")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("refusal codes with no wording in kernel.ts:\n  %s", strings.Join(missing, "\n  "))
	}
}

// codeOf reads the code a call passes, whether it is spelled at the call or
// named by a constant. Reading only literals is how a vocabulary moved into
// constants leaves the catalogue unchecked — the shared refusals did exactly
// that, and this test went on passing.
func codeOf(arg ast.Expr, consts map[string]string) string {
	switch v := arg.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return ""
		}
		code, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		return code
	case *ast.Ident:
		return consts[v.Name]
	}
	return ""
}

// packageStringConsts collects the package's string constants so a code named
// by one resolves to what it holds.
func packageStringConsts(t *testing.T, fset *token.FileSet, names []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range names {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if code, err := strconv.Unquote(lit.Value); err == nil {
							out[id.Name] = code
						}
					}
				}
			}
		}
	}
	return out
}
