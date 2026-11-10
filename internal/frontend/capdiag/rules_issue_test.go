package capdiag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestARuleFileTheHostSkipsIsReportedAsAnInstructionsIssue(t *testing.T) {
	root, home := testenv.TempDir(t), testenv.TempDir(t)
	dir := filepath.Join(root, ".reasonix", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte(strings.Repeat("x", 40*1024)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, issues := collectInstructions(root, home, func(p string) string { return p })
	for _, is := range issues {
		if is.Code == "rules.too_large" && is.Subsystem == "instructions" && strings.HasSuffix(is.Source, "big.md") {
			return
		}
	}
	t.Fatalf("issues = %+v, want rules.too_large naming big.md", issues)
}
