package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestIssue4038NFCNameResolvesNFDInstalledSkill(t *testing.T) {
	project := testenv.TempDir(t)
	target := filepath.Join(project, ".reasonix", "skills", "cafe\u0301", "SKILL.md")
	writeFile(t, target, "---\ndescription: fixture\n---\nbody")
	tool := NewTool(Options{ProjectRoot: project, HomeDir: testenv.TempDir(t)})
	resp := execInstall(t, tool, map[string]any{"op": "uninstall", "name": "café", "scope": "project"})
	if !resp.OK || len(resp.Actions) != 1 || resp.Actions[0].Action != "remove_skill" {
		t.Fatalf("NFC uninstall did not find NFD skill: %+v", resp)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("NFD skill still present after uninstall: %v", err)
	}
}
