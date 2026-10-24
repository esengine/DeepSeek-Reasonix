package checkenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDenyCoversEverySpellingTheFilesystemResolves(t *testing.T) {
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storage := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "Secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Secrets", "token"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(source, "secrets")); err != nil {
		t.Skip("filesystem distinguishes case")
	}
	denied, err := relativeDenies(source, []string{filepath.Join(source, "secrets")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := freezeTree(t.Context(), source, filepath.Join(storage, "copy"), treePolicy{source: true, denied: denied}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(storage, "copy", "Secrets", "token")); !os.IsNotExist(err) {
		t.Fatalf("a denied root spelled in another case was frozen: %v", err)
	}
}
