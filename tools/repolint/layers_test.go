package main

import "testing"

func TestLayeringContract(t *testing.T) {
	for _, tc := range []struct {
		name, pkg, dep string
		wantViolation  bool
	}{
		{"utility package stays a leaf", "internal/fileutil", "internal/config", true},
		{"utility package may use the stdlib only", "internal/textutil", "internal/agent", true},
		{"kernel may not reach the controller", "internal/agent", "internal/control", true},
		{"kernel may not reach a frontend", "internal/agent", "internal/cli", true},
		{"diagnostics may not reach the composition root", "internal/capdiag", "internal/boot", true},
		{"frontend may use the controller", "internal/serve", "internal/control", false},
		{"frontend may use another frontend", "internal/cli", "internal/serve", false},
		{"frontend subpackage may use its parent", "internal/bot/qq", "internal/bot", false},
		{"entrypoint may use a frontend", "cmd/reasonix", "internal/cli", false},
		{"desktop host may use the controller", "desktop", "internal/control", false},
		{"controller may use the kernel", "internal/control", "internal/agent", false},
		{"kernel may use a utility package", "internal/agent", "internal/fileutil", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := violates(tc.pkg, tc.dep) != ""; got != tc.wantViolation {
				t.Fatalf("violates(%q, %q) = %v, want %v", tc.pkg, tc.dep, got, tc.wantViolation)
			}
		})
	}
}

func TestLayeringReadsImportsFromSource(t *testing.T) {
	src := "package agent\n\nimport (\n\t\"fmt\"\n\t\"reasonix/internal/cli\"\n)\n\nvar _ = fmt.Sprint\nvar _ = cli.Run\n"
	s := parseBytes("internal/agent/a.go", []byte(src))
	found := checkLayering(map[string][]importRef{s.rel: s.importRefs()})
	if len(found) != 1 || found[0].Rule != ruleLayering || found[0].Line != 5 {
		t.Fatalf("want one %s on line 5, got %+v", ruleLayering, found)
	}
}

func TestLayeringIgnoresTestFiles(t *testing.T) {
	src := "package agent\n\nimport \"reasonix/internal/cli\"\n\nvar _ = cli.Run\n"
	s := parseBytes("internal/agent/a_test.go", []byte(src))
	if refs := s.importRefs(); len(refs) != 0 {
		t.Fatalf("test file imports should not be layered: %v", refs)
	}
}
