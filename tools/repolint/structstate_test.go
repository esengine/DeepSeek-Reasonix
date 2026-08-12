package main

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"
)

func structSource(guard string, scalars int) string {
	var b strings.Builder
	b.WriteString("package p\n\nimport (\n\t\"sync\"\n\t\"sync/atomic\"\n)\n\nvar _ = sync.Mutex{}\nvar _ = atomic.Bool{}\n\ntype T struct {\n")
	if guard != "" {
		fmt.Fprintf(&b, "\t%s\n", guard)
	}
	for i := range scalars {
		fmt.Fprintf(&b, "\tf%d bool\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

// The rule exists for state several goroutines reach. A record with many fields
// is describing many things, which is not the same defect and would bury it.
func TestStructStateIgnoresTypesWithoutASynchronisationPrimitive(t *testing.T) {
	s := parseBytes("t.go", []byte(structSource("", maxScalarFields*4)))
	if got := checkStructState(s); len(got) != 0 {
		t.Fatalf("a plain record was flagged: %v", got)
	}
}

func TestStructStateFlagsGuardedTypesPastTheCeiling(t *testing.T) {
	cases := []struct {
		guard string
		// An atomic guard is itself one of the scalars it makes concurrent, so
		// it counts twice over; a mutex guards other fields and counts as none.
		selfCounts int
	}{
		{"mu sync.Mutex", 0},
		{"mu sync.RWMutex", 0},
		{"mu *sync.Mutex", 0},
		{"ready atomic.Bool", 1},
	}
	for _, tc := range cases {
		t.Run(tc.guard, func(t *testing.T) {
			s := parseBytes("t.go", []byte(structSource(tc.guard, maxScalarFields+3)))
			found := checkStructState(s)
			if len(found) != 1 {
				t.Fatalf("guarded type past the ceiling produced %d findings, want 1", len(found))
			}
			if want := 3 + tc.selfCounts; found[0].Weight != want {
				t.Fatalf("weight = %d, want %d: the excess over the ceiling, so a worse struct outranks a better one",
					found[0].Weight, want)
			}
		})
	}
}

func TestStructStateCountsAtomicsAsScalars(t *testing.T) {
	src := "package p\n\nimport \"sync/atomic\"\n\ntype T struct {\n\ta atomic.Bool\n\tb atomic.Int64\n\tc atomic.Uint64\n}\n"
	s := parseBytes("t.go", []byte(src))
	if got := scalarFieldCount(firstStruct(t, s)); got != 3 {
		t.Fatalf("scalarFieldCount = %d, want 3: an atomic's concurrency contract is per-field", got)
	}
}

func TestStructStateCountsEachNameInAGroupedDeclaration(t *testing.T) {
	src := "package p\n\nimport \"sync\"\n\ntype T struct {\n\tmu sync.Mutex\n\ta, b, c bool\n}\n"
	s := parseBytes("t.go", []byte(src))
	if got := scalarFieldCount(firstStruct(t, s)); got != 3 {
		t.Fatalf("scalarFieldCount = %d, want 3: `a, b, c bool` is three independent flags", got)
	}
}

// Grouping by lifetime is the fix the message asks for, so it has to register:
// one sub-state field must replace the whole product it absorbed.
func TestStructStateFallsWhenScalarsMoveIntoASubState(t *testing.T) {
	before := parseBytes("t.go", []byte(structSource("mu sync.Mutex", maxScalarFields+5)))
	if len(checkStructState(before)) != 1 {
		t.Fatal("fixture did not exceed the ceiling")
	}
	after := parseBytes("t.go", []byte("package p\n\nimport \"sync\"\n\ntype sub struct{ a, b, c, d, e bool }\n\ntype T struct {\n\tmu sync.Mutex\n\tturn sub\n}\n"))
	if got := checkStructState(after); len(got) != 0 {
		t.Fatalf("grouping five flags into one sub-state still flagged: %v", got)
	}
}

func TestStructStateSkipsTestFiles(t *testing.T) {
	s := parseBytes("t_test.go", []byte(structSource("mu sync.Mutex", maxScalarFields*3)))
	if got := checkStructState(s); got != nil {
		t.Fatalf("test file measured: %v", got)
	}
}

func firstStruct(t *testing.T, s *sourceFile) *ast.StructType {
	t.Helper()
	var out *ast.StructType
	ast.Inspect(s.file, func(n ast.Node) bool {
		if st, ok := n.(*ast.StructType); ok && out == nil {
			out = st
		}
		return out == nil
	})
	if out == nil {
		t.Fatal("no struct in source")
	}
	return out
}
