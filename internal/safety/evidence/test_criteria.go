package evidence

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
)

// PathMayHoldTestCriteria reports whether a path could carry criteria the host
// holds. The name settles it without the bytes, so a caller walking a tree can
// decide what to read. Only criteria the host can re-run are held: a capture no
// backend evaluates is an obligation nothing discharges.
func PathMayHoldTestCriteria(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), "_test.go")
}

// HoldsTestCriteria reports whether a file carries criteria the host must
// already hold before anything may overwrite it. It reads the same contract
// RewrittenTestCriteria compares against: go test's own idea of what a test is.
func HoldsTestCriteria(path string, src []byte) bool {
	if !PathMayHoldTestCriteria(path) {
		return false
	}
	bodies, ok := testFunctionBodies(string(src))
	return ok && len(bodies) > 0
}

// RewrittenTestCriteria names the tests an edit changed the meaning of: one
// that existed before and now asserts something else, or one that is gone. A
// suite green afterwards is not the suite that was green before. Go test files
// and pytest modules are read, only when both sides parse; tests present only
// in the new file are additions.
func RewrittenTestCriteria(path, oldText, newText string) []string {
	var bodies func(string) (map[string]string, bool)
	switch {
	case PathMayHoldTestCriteria(path):
		bodies = testFunctionBodies
	case isPytestModule(path):
		bodies = pytestBodies
	default:
		return nil
	}
	before, okBefore := bodies(oldText)
	after, okAfter := bodies(newText)
	if !okBefore || !okAfter {
		return nil
	}
	var rewritten []string
	for name, body := range before {
		if next, kept := after[name]; !kept || next != body {
			rewritten = append(rewritten, name)
		}
	}
	slices.Sort(rewritten)
	return rewritten
}

// testFunctionBodies maps each test function to a signature of its syntax. The
// Test/Benchmark/Fuzz/Example prefixes are `go test`'s own contract for what a
// test is, not a guess about naming.
func testFunctionBodies(src string) (map[string]string, bool) {
	file, err := parser.ParseFile(token.NewFileSet(), "src.go", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	bodies := make(map[string]string, len(file.Decls))
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Recv != nil || !isTestFunctionName(fn.Name.Name) {
			continue
		}
		bodies[fn.Name.Name] = bodySignature(fn.Body)
	}
	return bodies, true
}

// bodySignature is the shape of a test body: node kinds in traversal order,
// carrying the names, literals and operators that decide what it asserts.
// Whitespace, comments and line breaks are not in it, so reformatting a test
// cannot read as rewriting one — and changing a single expected value must.
func bodySignature(body *ast.BlockStmt) string {
	var b strings.Builder
	ast.Inspect(body, func(n ast.Node) bool {
		switch t := n.(type) {
		case nil:
			return false
		case *ast.Ident:
			b.WriteString("i:" + t.Name + ";")
		case *ast.BasicLit:
			b.WriteString("l:" + t.Value + ";")
		case *ast.BinaryExpr:
			b.WriteString("o:" + t.Op.String() + ";")
		case *ast.UnaryExpr:
			b.WriteString("u:" + t.Op.String() + ";")
		default:
			fmt.Fprintf(&b, "%T;", n)
		}
		return true
	})
	return b.String()
}

func isTestFunctionName(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if after, ok := strings.CutPrefix(name, prefix); ok {
			return after == "" || !strings.HasPrefix(after, strings.ToLower(after[:1]))
		}
	}
	return false
}

// RewrittenCriteria names every existing test this turn rewrote or removed, in
// the order the ledger saw them. The completion summary reports it: a run that
// moved its own checks and a run that met the ones it was given both end with a
// green suite, and only this tells them apart.
func (l *Ledger) RewrittenCriteria() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, r := range l.receipts {
		if !r.Success {
			continue
		}
		for _, name := range r.CriteriaRewritten {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}
