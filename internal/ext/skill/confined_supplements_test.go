package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestLiveSkillReadsOnlyConfinedSupplements(t *testing.T) {
	for _, outside := range []bool{false, true} {
		t.Run(map[bool]string{false: "contained", true: "escaped"}[outside], func(t *testing.T) {
			root := testenv.TempDir(t)
			if err := os.Mkdir(filepath.Join(root, "references"), 0o700); err != nil {
				t.Fatal(err)
			}
			skillPath := filepath.Join(root, "SKILL.md")
			if err := os.WriteFile(skillPath, []byte("BODY"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "guide.md")
			if outside {
				target = filepath.Join(testenv.TempDir(t), "guide.md")
			}
			if err := os.WriteFile(target, []byte("REFERENCE BODY"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(root, "references", "guide.md")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			sk := (Skill{Name: "fixture", Path: skillPath, Body: "BODY", supplementsDeferred: true, confinedSupplements: true}).Complete()
			if outside {
				if !errors.Is(sk.completionError, ErrInvocationUnavailable) || strings.Contains(sk.Body, "REFERENCE BODY") {
					t.Fatalf("escaped reference read: %q %v", sk.Body, sk.completionError)
				}
			} else if sk.completionError != nil || !strings.Contains(sk.Body, "REFERENCE BODY") {
				t.Fatalf("contained reference failed: %q %v", sk.Body, sk.completionError)
			}
		})
	}
}
