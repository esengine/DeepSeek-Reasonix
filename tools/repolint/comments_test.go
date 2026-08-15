package main

import (
	"strings"
	"testing"
)

func rules(t *testing.T, rel, src string) []string {
	t.Helper()
	s := parseBytes(rel, []byte(src))
	if s == nil {
		t.Fatalf("parse %s failed", rel)
	}
	var out []string
	for _, f := range checkComments(s) {
		out = append(out, f.Rule)
	}
	return out
}

func TestCgoPreambleIsCompilerInputNotProse(t *testing.T) {
	src := "package main\n\n/*\n#include <stdint.h>\n\nstatic void tick(void) {\n\tif (x != NULL) {\n\t\treturn;\n\t}\n\tstart(1);\n}\n*/\nimport \"C\"\n"
	if got := rules(t, "watchdog.go", src); len(got) != 0 {
		t.Fatalf("cgo preamble flagged: %v", got)
	}
}

func TestTrailingCommentIsNotDeadCode(t *testing.T) {
	src := "package p\n\ntype T struct {\n\tKind string // tool | plan | recovery; empty = tool\n\tSum  string // ssh.FingerprintSHA256(key)\n}\n"
	if got := rules(t, "t.go", src); len(got) != 0 {
		t.Fatalf("trailing field comments flagged: %v", got)
	}
}

func TestDocCommentCodeBlockIsNotDeadCode(t *testing.T) {
	src := "package p\n\n// Run does a thing:\n//\n//\tx := Run(ctx)\nfunc Run() {}\n"
	if got := rules(t, "t.go", src); len(got) != 0 {
		t.Fatalf("doc code block flagged: %v", got)
	}
}

func TestDeclarationDocsAllowDetailedContractsButRemainBounded(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("// Run explains a non-obvious contract.\n")
	for range capDeclDoc - 1 {
		doc.WriteString("// Additional contract detail.\n")
	}
	clean := "package p\n\n" + doc.String() + "func Run() {}\n"
	if got := rules(t, "t.go", clean); len(got) != 0 {
		t.Fatalf("%d-line declaration doc flagged: %v", capDeclDoc, got)
	}

	doc.WriteString("// One line beyond the declaration-doc limit.\n")
	tooLong := "package p\n\n" + doc.String() + "func Run() {}\n"
	found := checkComments(parseBytes("t.go", []byte(tooLong)))
	if len(found) != 1 || found[0].Rule != ruleEssay || found[0].Weight != 1 {
		t.Fatalf("want one 1-line %s excess, got %+v", ruleEssay, found)
	}
}

func TestFloatingCommentsKeepTheShortLimit(t *testing.T) {
	src := "package p\n\nfunc Run() {\n\t// one\n\t// two\n\t// three\n\t// four\n\t_ = 0\n}\n"
	found := checkComments(parseBytes("t.go", []byte(src)))
	if len(found) != 1 || found[0].Rule != ruleEssay || found[0].Weight != 1 {
		t.Fatalf("want one 1-line %s excess, got %+v", ruleEssay, found)
	}
}

func TestCommentedOutCodeInBodyIsFlagged(t *testing.T) {
	src := "package p\n\nfunc Run() {\n\t// old := compute(1)\n\t_ = 0\n}\n"
	if got := rules(t, "t.go", src); len(got) != 1 || got[0] != ruleDeadCode {
		t.Fatalf("want one %s, got %v", ruleDeadCode, got)
	}
}

func TestEssayWeightIsTheExcessOverTheLimit(t *testing.T) {
	src := "package p\n\nfunc Run() {\n\t// one\n\t// two\n\t// three\n\t// four\n\t// five\n\t_ = 0\n}\n"
	s := parseBytes("t.go", []byte(src))
	found := checkComments(s)
	if len(found) != 1 || found[0].Rule != ruleEssay {
		t.Fatalf("want one %s, got %v", ruleEssay, found)
	}
	if found[0].Weight != 5-capFloating {
		t.Fatalf("weight = %d, want %d", found[0].Weight, 5-capFloating)
	}
}

func TestDocGoCarriesTheLongPackageExplanation(t *testing.T) {
	long := "package p\n"
	var doc strings.Builder
	doc.WriteString("// Package p explains itself.\n")
	for range capPackageDoc + 2 {
		doc.WriteString("//\n// more prose\n")
	}
	if got := rules(t, "sub/doc.go", doc.String()+long); len(got) != 0 {
		t.Fatalf("doc.go package comment flagged: %v", got)
	}
	if got := rules(t, "sub/other.go", doc.String()+long); len(got) == 0 {
		t.Fatal("same package comment outside doc.go should be flagged")
	}
}

func TestMarkersAndNarrative(t *testing.T) {
	for _, tc := range []struct {
		name, comment, want string
	}{
		{"bare todo", "// TODO: later", ruleMarker},
		{"anchored todo", "// TODO(#312): later", ""},
		{"fixme", "// FIXME later", ruleMarker},
		{"stage narrative", "// Stage 6b2 hands off the prompt", ruleNarrative},
		{"lowercase hack prose", "// this is a hack around a driver bug", ""},
		{"banner", "// ----------------", ruleBanner},
		{"labelled banner", "// ─── helpers ───", ruleBanner},
		{"ascii labelled banner", "// ==== setup ====", ruleBanner},
		{"prose with ellipsis", "// waits for the child... then reaps it", ""},
		{"prose with a dash", "// clamp to width-1 — Yoga miscounts wrap", ""},
		{"build directive", "//go:generate stringer -type=T", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := rules(t, "t.go", "package p\n\n"+tc.comment+"\nfunc Run() {}\n")
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("want clean, got %v", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("want %s, got %v", tc.want, got)
			}
		})
	}
}
