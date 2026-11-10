package skill

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestWindowsLiveSkillJunctions(t *testing.T) {
	for _, outside := range []bool{false, true} {
		t.Run(map[bool]string{false: "contained", true: "escaped"}[outside], func(t *testing.T) {
			root := testenv.TempDir(t)
			target := filepath.Join(root, "data")
			if outside {
				target = filepath.Join(testenv.TempDir(t), "data")
			}
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			for path, body := range map[string]string{
				filepath.Join(root, "SKILL.md"): "BODY", filepath.Join(target, "guide.md"): "REFERENCE BODY",
			} {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			linkedRoot := filepath.Join(testenv.TempDir(t), "skill")
			for link, source := range map[string]string{
				filepath.Join(root, "references"): target,
				linkedRoot:                        root,
			} {
				if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, source).CombinedOutput(); err != nil {
					t.Fatalf("create junction: %v: %s", err, out)
				}
			}
			sk := (Skill{Name: "fixture", Path: filepath.Join(linkedRoot, "SKILL.md"), Body: "BODY", supplementsDeferred: true, confinedSupplements: true}).Complete()
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
