package main

import (
	"strings"
	"testing"
)

func linesOf(n int) []byte { return []byte(strings.Repeat("x\n", n)) }

func goLinesOf(n int) []byte {
	return []byte("package a\n" + strings.Repeat("var _ = 1\n", n))
}

func TestSizeRuleCoversTypeScript(t *testing.T) {
	for _, tc := range []struct {
		name, rel string
		data      []byte
		wantRule  string
	}{
		{"tsx over the ceiling", "desktop/frontend/src/components/Big.tsx", linesOf(900), ruleFileSize},
		{"ts over the ceiling", "desktop/frontend/src/lib/big.ts", linesOf(900), ruleFileSize},
		{"tsx under the ceiling", "desktop/frontend/src/components/Small.tsx", linesOf(100), ""},
		{"front-end test dir is exempt", "desktop/frontend/src/__tests__/big.test.ts", linesOf(900), ""},
		{"spec suffix is exempt", "desktop/frontend/src/lib/big.spec.ts", linesOf(900), ""},
		{"go test file is exempt", "internal/runtime/agent/big_test.go", goLinesOf(900), ""},
		{"worker ts", "workers/crash-report/src/big.ts", linesOf(900), ruleFileSize},
		{"go file keeps its rule", "internal/runtime/agent/big.go", goLinesOf(900), ruleFileSize},
		{"locale table is exempt", "desktop/frontend-next/src/i18n/en.ts", linesOf(3000), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := parseBytes(tc.rel, tc.data)
			if s == nil {
				t.Fatal("parseBytes returned nil")
			}
			found := checkSize(s)
			if tc.wantRule == "" {
				if len(found) != 0 {
					t.Fatalf("want no finding, got %+v", found)
				}
				return
			}
			if len(found) != 1 || found[0].Rule != tc.wantRule {
				t.Fatalf("want one %s finding, got %+v", tc.wantRule, found)
			}
		})
	}
}

// The comment and layering rules are written against the Go AST, so a
// TypeScript file must reach checkSize without ever being handed to them.
func TestTypeScriptCarriesNoGoAST(t *testing.T) {
	src := "// ===== section banner =====\nimport { x } from './y'\nexport const a = x\n"
	s := parseBytes("desktop/frontend/src/lib/a.ts", []byte(src))
	if s == nil {
		t.Fatal("a TypeScript file must still be read for the size rule")
	}
	if s.file != nil || s.fset != nil {
		t.Fatal("a TypeScript file must not carry a Go AST")
	}
	if refs := s.importRefs(); len(refs) != 0 {
		t.Fatalf("TypeScript imports must stay out of the layering graph: %v", refs)
	}
	if s.lines != 3 {
		t.Fatalf("lines = %d, want 3", s.lines)
	}
}

func TestLintableExtensions(t *testing.T) {
	for name, want := range map[string]bool{
		"a.go":         true,
		"a.ts":         true,
		"a.tsx":        true,
		"a.js":         false,
		"a.json":       false,
		"a.css":        false,
		"a.md":         false,
		"tsconfig.tsx": true,
	} {
		if got := lintable(name); got != want {
			t.Errorf("lintable(%q) = %v, want %v", name, got, want)
		}
	}
}
