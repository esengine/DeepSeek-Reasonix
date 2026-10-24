//go:build linux

package testenv

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// VerificationRuntime supplies an offline Bash image with its installed ELF
// dependencies. It exposes no host home, cache, service or credential directory.
func VerificationRuntime(t *testing.T) string {
	t.Helper()
	image := t.TempDir()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	InstallVerificationBinary(t, image, bash, "/bin/bash")
	return image
}

func InstallVerificationBinary(t *testing.T, image, executable, virtual string) {
	t.Helper()
	copyFile := func(source, target string) {
		t.Helper()
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(image, strings.TrimPrefix(target, "/"))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0755); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(executable, virtual)
	binary, err := elf.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := binary.ImportedLibraries()
	binary.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(libraries) == 0 {
		return
	}
	cmd := exec.CommandContext(t.Context(), "ldd", executable)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ldd: %v %s", err, out)
	}
	for field := range strings.FieldsSeq(string(out)) {
		if filepath.IsAbs(field) {
			copyFile(field, field)
		}
	}
}
