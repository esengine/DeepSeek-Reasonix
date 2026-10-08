package main

import (
	"fmt"
	"strings"
	"testing"
)

// typeSpanSource returns a package source whose receiver type (recv is the full
// receiver type expr, e.g. "*T" or "T") carries n methods, so a test can dial a
// type's size without hand-writing dozens of funcs.
func typeSpanSource(rel, recv string, methods int) *sourceFile {
	var b strings.Builder
	b.WriteString("package p\n\n")
	for i := 0; i < methods; i++ {
		fmt.Fprintf(&b, "func (%s) M%d() {}\n", recv, i)
	}
	return parseBytes(rel, []byte(b.String()))
}

// The defect is a type too big to hold in one head, not a function too long:
// 45 one-line methods in a single file must trip the method ceiling even though
// no function rule would.
func TestTypeSpanFlagsGodTypeByMethodCount(t *testing.T) {
	scan := newTypeSpanScan()
	scan.observe(typeSpanSource("internal/x/app.go", "*T", maxTypeMethods+5))
	got := scan.findings()
	if len(got) != 1 {
		t.Fatalf("god type produced %d findings, want 1", len(got))
	}
	if got[0].Weight != 5 {
		t.Fatalf("weight = %d, want 5: the excess over the ceiling", got[0].Weight)
	}
	if got[0].File != "internal/x" || got[0].Rule != ruleTypeSpan {
		t.Fatalf("finding mislabeled: %+v", got[0])
	}
}

// A type under the method ceiling but smeared across a dozen files is the same
// loss of cohesion; the file axis must catch what the method axis cannot.
func TestTypeSpanFlagsGodTypeByFileSpread(t *testing.T) {
	scan := newTypeSpanScan()
	for i := 0; i < maxTypeFiles+3; i++ {
		// One method per file keeps the total under the method ceiling so only
		// the file axis can trip.
		scan.observe(typeSpanSource(fmt.Sprintf("pkg/f%d.go", i), "*T", 1))
	}
	got := scan.findings()
	if len(got) != 1 {
		t.Fatalf("file-sprawled type produced %d findings, want 1", len(got))
	}
	if got[0].Weight != 3 {
		t.Fatalf("weight = %d, want 3: the number of files over the ceiling", got[0].Weight)
	}
}

// Methods of one type live in many files by design in Go; the scan must sum them
// before comparing, or the rule would never see a god type.
func TestTypeSpanAggregatesAcrossFiles(t *testing.T) {
	scan := newTypeSpanScan()
	scan.observe(typeSpanSource("pkg/a.go", "*T", 20))
	scan.observe(typeSpanSource("pkg/b.go", "*T", 25))
	got := scan.findings()
	if len(got) != 1 {
		t.Fatalf("cross-file type produced %d findings, want 1", len(got))
	}
	if got[0].Weight != 5 {
		t.Fatalf("weight = %d, want 5: 45 methods over the 40 ceiling", got[0].Weight)
	}
}

// A pointer and a value receiver name the same type; counting them apart would
// halve the real method count and hide the defect.
func TestTypeSpanUnifiesPointerAndValueReceivers(t *testing.T) {
	scan := newTypeSpanScan()
	scan.observe(typeSpanSource("pkg/a.go", "*T", 20))
	scan.observe(typeSpanSource("pkg/b.go", "T", 25))
	if got := scan.findings(); len(got) != 1 || got[0].Weight != 5 {
		t.Fatalf("pointer/value receivers not unified: %v", got)
	}
}

// Test files exercise a type, they are not the product surface the ceiling
// bounds, so a type whose only methods are in _test.go must stay invisible.
func TestTypeSpanSkipsTestFiles(t *testing.T) {
	scan := newTypeSpanScan()
	scan.observe(typeSpanSource("pkg/t_test.go", "*T", maxTypeMethods*3))
	if got := scan.findings(); len(got) != 0 {
		t.Fatalf("test methods counted as product surface: %v", got)
	}
}

// The ratchet's point: splitting one god type into two normal ones must clear
// the finding, so the fix the message asks for actually registers.
func TestTypeSpanClearsWhenTypeIsSplit(t *testing.T) {
	scan := newTypeSpanScan()
	scan.observe(typeSpanSource("pkg/a.go", "*A", 25))
	scan.observe(typeSpanSource("pkg/b.go", "*B", 25))
	if got := scan.findings(); len(got) != 0 {
		t.Fatalf("two modest types flagged as god types: %v", got)
	}
}

// A type comfortably inside both ceilings is the common case and must be silent.
func TestTypeSpanIgnoresModestType(t *testing.T) {
	scan := newTypeSpanScan()
	scan.observe(typeSpanSource("pkg/t.go", "*T", 10))
	scan.observe(typeSpanSource("pkg/u.go", "*T", 5))
	if got := scan.findings(); len(got) != 0 {
		t.Fatalf("modest type flagged: %v", got)
	}
}
