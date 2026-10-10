package main

import "testing"

// processInputFindings parses through parseBytes, the mode the real run uses.
func processInputFindings(t *testing.T, rel, src string) []Finding {
	t.Helper()
	file := parseBytes(rel, []byte(src))
	if file == nil || file.file == nil {
		t.Fatalf("%s did not parse", rel)
	}
	return checkProcessInput(file)
}

func TestProcessInputReadBelowTheFrontendsIsReported(t *testing.T) {
	src := `package boot
import "os"
func f() string { h, _ := os.UserHomeDir(); return h + os.Getenv("X") }
func g(lookup func(string) string) {}
func h() { g(os.Getenv) }`
	got := processInputFindings(t, "internal/assembly/boot/x.go", src)
	if len(got) != 3 || got[0].Rule != ruleProcessInput {
		t.Fatalf("findings = %+v, want each call and the value reference", got)
	}
}

func TestProcessInputFollowsTheImportName(t *testing.T) {
	src := `package boot
import sys "os"
func f() string { return sys.Getenv("X") }`
	if got := processInputFindings(t, "internal/assembly/boot/x.go", src); len(got) != 1 {
		t.Fatalf("findings = %+v, want the renamed import's read", got)
	}
}

func TestProcessInputLeavesFrontendsHostsAndTestsAlone(t *testing.T) {
	src := `package x
import "os"
func f() string { return os.Getenv("X") }`
	for _, rel := range []string{
		"internal/frontend/cli/x.go",
		"cmd/reasonix/x.go",
		"internal/assembly/boot/x_test.go",
	} {
		if got := processInputFindings(t, rel, src); len(got) != 0 {
			t.Fatalf("%s: findings = %+v, want none", rel, got)
		}
	}
}

func TestProcessInputCountsALocalShadowingOs(t *testing.T) {
	src := `package boot
import "os"
type env struct{}
func (env) Getenv(string) string { return "" }
func f() string { os := env{}; return os.Getenv("X") }
var _ = os.Args`
	if got := processInputFindings(t, "internal/assembly/boot/x.go", src); len(got) != 1 {
		t.Fatalf("findings = %+v, want the shadowed read counted: names are not resolved", got)
	}
}
