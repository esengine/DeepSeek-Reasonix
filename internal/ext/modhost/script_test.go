package modhost

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestTheScriptIsWrittenUnderAContentAddressedNameAndRepairedIfChanged(t *testing.T) {
	dir := testenv.TempDir(t)
	path, err := WriteScript(dir)
	if err != nil || filepath.Base(path) != ScriptName() {
		t.Fatalf("path = %q, %v", path, err)
	}
	if err := os.WriteFile(path, []byte("process.exit(0)"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := WriteScript(dir)
	if err != nil || again != path {
		t.Fatalf("second write = %q, %v", again, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(hostScript) {
		t.Fatal("a tampered script file was run as it was found instead of being rewritten")
	}
}
