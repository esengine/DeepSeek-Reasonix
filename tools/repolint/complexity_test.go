package main

import (
	"go/ast"
	"strings"
	"testing"
)

func firstFunc(t *testing.T, src string) (*sourceFile, *ast.FuncDecl) {
	t.Helper()
	s := parseBytes("t.go", []byte(src))
	if s == nil {
		t.Fatal("parse failed")
	}
	for _, decl := range s.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return s, fn
		}
	}
	t.Fatal("no function in source")
	return nil, nil
}

func TestCyclomaticIgnoresDefaultClause(t *testing.T) {
	_, fn := firstFunc(t, "package p\n\nfunc f(x int) int {\n\tswitch x {\n\tcase 1:\n\t\treturn 1\n\tcase 2:\n\t\treturn 2\n\tdefault:\n\t\treturn 0\n\t}\n}\n")
	if got := cyclomatic(fn.Body); got != 3 {
		t.Fatalf("cyclomatic = %d, want 3: one base plus two real cases, default is not a branch", got)
	}
}

func TestCyclomaticCountsShortCircuitOperators(t *testing.T) {
	_, fn := firstFunc(t, "package p\n\nfunc f(a, b bool) bool {\n\treturn a && b || !a\n}\n")
	if got := cyclomatic(fn.Body); got != 3 {
		t.Fatalf("cyclomatic = %d, want 3: one base plus && and ||", got)
	}
}

func TestComplexityFlagsOnlyWhatExceedsTheLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n\nfunc f(x int) int {\n")
	for range maxComplexity {
		b.WriteString("\tif x > 0 {\n\t\tx--\n\t}\n")
	}
	b.WriteString("\treturn x\n}\n")
	s := parseBytes("t.go", []byte(b.String()))
	found := checkComplexity(s)
	if len(found) == 0 {
		t.Fatal("a function past both limits was not flagged")
	}
	for _, f := range found {
		if f.Weight < 1 {
			t.Fatalf("%s weight = %d, want the excess over the limit", f.Rule, f.Weight)
		}
	}
}

func TestShortSimpleFunctionIsClean(t *testing.T) {
	s := parseBytes("t.go", []byte("package p\n\nfunc f() int { return 1 }\n"))
	if got := checkComplexity(s); len(got) != 0 {
		t.Fatalf("clean function flagged: %v", got)
	}
}

func TestTestFilesAreNotMeasured(t *testing.T) {
	s := parseBytes("t_test.go", []byte("package p\n\nfunc f() int { return 1 }\n"))
	if got := checkComplexity(s); got != nil {
		t.Fatalf("test file measured: %v", got)
	}
}

func TestMethodNamesCarryTheirReceiver(t *testing.T) {
	_, fn := firstFunc(t, "package p\n\ntype T struct{}\n\nfunc (t *T) Do() {}\n")
	if got := funcName(fn); got != "T.Do" {
		t.Fatalf("funcName = %q, want T.Do", got)
	}
}
