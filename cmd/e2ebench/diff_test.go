package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTestsAllowsStaticEnvAssignment(t *testing.T) {
	repo := t.TempDir()
	writeE2EFile(t, filepath.Join(repo, "go.mod"), "module example.com/e2ebenchtest\n\ngo 1.23\n")
	writeE2EFile(t, filepath.Join(repo, "env_test.go"), `package e2ebenchtest

import (
	"os"
	"testing"
)

func TestEnv(t *testing.T) {
	if got := os.Getenv("REASONIX_E2E_ENV"); got != "ok" {
		t.Fatalf("REASONIX_E2E_ENV = %q", got)
	}
}
`)

	ok, out := runTests(repo, "GOWORK=off REASONIX_E2E_ENV=ok go test", []string{"./..."})
	if !ok {
		t.Fatalf("runTests failed:\n%s", out)
	}
}

func TestRunTestsRejectsDynamicEnvAssignment(t *testing.T) {
	ok, out := runTests(t.TempDir(), "REASONIX_E2E_ENV=$(echo ok) go test", []string{"./..."})
	if ok {
		t.Fatal("runTests accepted dynamic env assignment")
	}
	if !strings.Contains(out, "invalid test command: shell expansion") {
		t.Fatalf("output = %q, want shell expansion rejection", out)
	}
}

func writeE2EFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
