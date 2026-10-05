package main

import (
	"fmt"
	"go/ast"
	"strings"
)

// An exported kernel API with no production caller anywhere in the tree is how
// a capability dies quietly: deleting the one frontend that called it leaves
// the implementation, its tests, and its doc comments all intact and green.
// That is what left config recovery reading a snapshot nothing wrote. Tests do
// not count as callers — they are exactly what keeps such a corpse warm.
type orphanScan struct {
	decls    map[string][]orphanDecl
	uses     map[string]int
	testUses map[string]int
	// into redirects identifier counting while a test file is walked.
	into map[string]int
}

type orphanDecl struct {
	file string
	line int
	kind string
}

func newOrphanScan() *orphanScan {
	return &orphanScan{decls: map[string][]orphanDecl{}, uses: map[string]int{}, testUses: map[string]int{}}
}

// observe records one file's exported kernel declarations and, separately, every
// identifier it reads. Both halves span the whole tree — the desktop module is
// a caller of internal/, so a scan that stopped at one module would call its
// APIs dead.
func (o *orphanScan) observe(src *sourceFile) {
	if src.file == nil {
		return
	}
	if strings.HasSuffix(src.rel, "_test.go") {
		o.into = o.testUses
		defer func() { o.into = nil }()
		ast.Inspect(src.file, func(n ast.Node) bool { o.countIdent(n); return true })
		return
	}
	kernel := strings.HasPrefix(src.rel, "internal/") && !testSupportPkg(src.rel)
	for _, decl := range src.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		name := fn.Name.Name
		// Functions only. A method can be reached through any interface that
		// happens to describe it, including one a third-party package owns and
		// calls — chatTUI.Init is bubbletea's, and no file here names it. Proving
		// that needs the type information an AST pass does not have, and the cost
		// of guessing wrong is deleting live code.
		if kernel && fn.Recv == nil && ast.IsExported(name) && !orphanExempt(name) {
			o.decls[name] = append(o.decls[name], orphanDecl{
				file: src.rel, line: src.fset.Position(fn.Pos()).Line, kind: "func",
			})
		}
	}
	ast.Inspect(src.file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			// The declaration's own name is not a use of it; its body is.
			if v.Recv != nil {
				ast.Inspect(v.Recv, func(r ast.Node) bool { o.countIdent(r); return true })
			}
			if v.Type != nil {
				ast.Inspect(v.Type, func(t ast.Node) bool { o.countIdent(t); return true })
			}
			if v.Body != nil {
				ast.Inspect(v.Body, func(b ast.Node) bool { o.countIdent(b); return true })
			}
			return false
		default:
			o.countIdent(n)
		}
		return true
	})
}

func (o *orphanScan) countIdent(n ast.Node) {
	id, ok := n.(*ast.Ident)
	if !ok {
		return
	}
	if o.into != nil {
		o.into[id.Name]++
		return
	}
	o.uses[id.Name]++
}

// testSupportPkg marks packages that exist to serve tests across package
// boundaries. Their exports cannot move into _test.go, so "no production
// caller" is their normal state rather than a finding.
func testSupportPkg(rel string) bool {
	dir := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dir = rel[:i]
	}
	base := dir
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		base = dir[i+1:]
	}
	return strings.HasPrefix(base, "test") || strings.HasSuffix(base, "test") || strings.HasSuffix(base, "testutil")
}

// orphanExempt skips names a runtime calls for us, where no source reference
// proves anything either way.
func orphanExempt(name string) bool {
	return name == "Main" || name == "TestMain"
}

// findings reports every exported kernel function whose only appearances in
// non-test code are its own declarations.
func (o *orphanScan) findings() []Finding {
	var out []Finding
	for name, decls := range o.decls {
		if o.uses[name] > 0 {
			continue
		}
		// Tests referencing it means it is either a cross-package fixture or a
		// corpse its own tests keep warm, and only a reader can tell those
		// apart. Nothing referencing it at all is unambiguous.
		what := "is referenced only by tests; wire it, move it to a test-support package, or delete it"
		if o.testUses[name] == 0 {
			what = "is referenced nowhere, including tests; delete it"
		}
		for _, d := range decls {
			out = append(out, Finding{
				File: d.file, Line: d.line, Rule: ruleOrphan, Weight: 1,
				Msg: fmt.Sprintf("exported %s %s %s", d.kind, name, what),
			})
		}
	}
	return out
}
