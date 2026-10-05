package main

import "testing"

func TestLayeringContract(t *testing.T) {
	for _, tc := range []struct {
		name, pkg, dep string
		wantViolation  bool
	}{
		{"utility package stays a leaf", "internal/base/fileutil", "internal/contract/config", true},
		{"a utility may use another utility", "internal/base/fileutil", "internal/base/textutil", false},
		{"utility package may use the stdlib only", "internal/base/textutil", "internal/runtime/agent", true},
		{"kernel may not reach the controller", "internal/runtime/agent", "internal/session/control", true},
		{"kernel may not reach a frontend", "internal/runtime/agent", "internal/frontend/cli", true},
		{"diagnostics may not reach the composition root", "internal/frontend/capdiag", "internal/assembly/boot", true},
		{"frontend may use the controller", "internal/frontend/serve", "internal/session/control", false},
		{"frontend may use another frontend", "internal/frontend/cli", "internal/frontend/serve", false},
		{"frontend subpackage may use its parent", "internal/frontend/serve/hub", "internal/frontend/serve", false},
		{"entrypoint may use a frontend", "cmd/reasonix", "internal/frontend/cli", false},
		{"desktop host may use the controller", "desktop", "internal/session/control", false},
		{"controller may use the kernel", "internal/session/control", "internal/runtime/agent", false},
		{"kernel may use a utility package", "internal/runtime/agent", "internal/base/fileutil", false},
		{"host adapter may use the frontend whose port it implements", "internal/frontend/remotehost", "internal/frontend/serve", false},
		{"host adapter may use the kernel below it", "internal/frontend/remotehost", "internal/platform/remote/attach", false},
		{"a host assembles a host adapter", "cmd/reasonix-studio-host", "internal/frontend/remotehost", false},
		{"a shell assembles a host adapter", "desktop/next", "internal/frontend/remotehost", false},
		{"the kernel may not reach a host adapter", "internal/runtime/agent", "internal/frontend/remotehost", true},
		{"the controller may not reach a host adapter", "internal/session/control", "internal/frontend/remotehost", true},
		{"a utility may not reach a host adapter", "internal/base/fileutil", "internal/frontend/remotehost", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := violates(tc.pkg, tc.dep) != ""; got != tc.wantViolation {
				t.Fatalf("violates(%q, %q) = %v, want %v", tc.pkg, tc.dep, got, tc.wantViolation)
			}
		})
	}
}

func TestLayeringReadsImportsFromSource(t *testing.T) {
	src := "package agent\n\nimport (\n\t\"fmt\"\n\t\"reasonix/internal/frontend/cli\"\n)\n\nvar _ = fmt.Sprint\nvar _ = cli.Run\n"
	s := parseBytes("internal/runtime/agent/a.go", []byte(src))
	found := checkLayering(map[string][]importRef{s.rel: s.importRefs()})
	if len(found) != 1 || found[0].Rule != ruleLayering || found[0].Line != 5 {
		t.Fatalf("want one %s on line 5, got %+v", ruleLayering, found)
	}
}

func TestLayeringIgnoresTestFiles(t *testing.T) {
	src := "package agent\n\nimport \"reasonix/internal/frontend/cli\"\n\nvar _ = cli.Run\n"
	s := parseBytes("internal/runtime/agent/a_test.go", []byte(src))
	if refs := s.importRefs(); len(refs) != 0 {
		t.Fatalf("test file imports should not be layered: %v", refs)
	}
}
