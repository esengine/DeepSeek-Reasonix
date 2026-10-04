package main

import (
	"fmt"
	"go/ast"
	"path"
)

// A god type hides from every file-scoped rule: split its methods across enough
// files and each one stays under the line and complexity ceilings while the type
// itself accrues hundreds of methods. Aggregating methods by receiver type —
// what go/types resolves a method to — exposes it; this is the cross-file half
// of the size budget that checkComplexity and checkStructState cannot see.
const (
	maxTypeMethods = 40
	maxTypeFiles   = 8
)

type typeSpanKey struct {
	dir  string
	name string
}

type typeSpan struct {
	methods int
	files   map[string]bool
}

type typeSpanScan struct {
	types map[typeSpanKey]*typeSpan
}

func newTypeSpanScan() *typeSpanScan {
	return &typeSpanScan{types: map[typeSpanKey]*typeSpan{}}
}

// observe records, per package, how many methods each receiver type carries and
// how many files they land in. Test files are excluded: a type's test surface
// is not the product surface the ceiling is meant to bound.
func (t *typeSpanScan) observe(src *sourceFile) {
	if src.file == nil || src.isTest() {
		return
	}
	// collect() yields slash-relative rels, so path.Dir gives the package dir;
	// "." means a root-package type with no directory component of its own.
	dir := path.Dir(src.rel)
	if dir == "." {
		dir = ""
	}
	for _, decl := range src.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		name := receiverName(fn.Recv.List[0].Type)
		if name == "?" {
			continue
		}
		key := typeSpanKey{dir: dir, name: name}
		ts := t.types[key]
		if ts == nil {
			ts = &typeSpan{files: map[string]bool{}}
			t.types[key] = ts
		}
		ts.methods++
		ts.files[src.rel] = true
	}
}

func typeSpanLabel(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

// findings reports each type that crosses a ceiling. Both axes ratchet on their
// own weight, so trimming either one relaxes the budget without the other having
// to move.
func (t *typeSpanScan) findings() []Finding {
	var out []Finding
	for key, ts := range t.types {
		label := typeSpanLabel(key.dir, key.name)
		file := key.dir
		if file == "" {
			file = "."
		}
		if ts.methods > maxTypeMethods {
			out = append(out, Finding{
				File: file, Line: 1, Rule: ruleTypeSpan, Weight: ts.methods - maxTypeMethods,
				Msg: fmt.Sprintf("%s has %d methods across %d files, over the %d-method ceiling; split the type",
					label, ts.methods, len(ts.files), maxTypeMethods),
			})
		}
		if len(ts.files) > maxTypeFiles {
			out = append(out, Finding{
				File: file, Line: 1, Rule: ruleTypeSpan, Weight: len(ts.files) - maxTypeFiles,
				Msg: fmt.Sprintf("%s spans %d files (%d methods), over the %d-file ceiling; consolidate or split the type",
					label, len(ts.files), ts.methods, maxTypeFiles),
			})
		}
	}
	return out
}
