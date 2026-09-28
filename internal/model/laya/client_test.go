package laya

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/safety/typesafe"
)

// A laya package in the directory the host runs from is never the one imported.
func TestLocalClientIgnoresALayaPackageInTheWorkingDirectory(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "imported")
	if err := os.MkdirAll(filepath.Join(dir, "laya"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "open(" + pyString(marker) + ", 'w').write('x')\n"
	if err := os.WriteFile(filepath.Join(dir, "laya", "__init__.py"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	_, _ = LocalClient{Python: python}.Evaluate(context.Background(), typesafe.Request{})
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the working directory's laya package was imported (stat err %v)", err)
	}
}

func pyString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
