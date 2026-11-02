//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCarriedProofRejectsLinkReferents(t *testing.T) {
	for _, kind := range []string{"same_bytes", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			a, path := provedAgent(t)
			target := filepath.Join(filepath.Dir(path), "target.go")
			if err := os.Rename(path, target); err != nil {
				t.Fatal(err)
			}
			if kind == "dangling" {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if a.pathFingerprint(path) != "" || a.carriedProof() != nil {
				t.Fatal("symlink referent acquired a reusable proof fingerprint")
			}
		})
	}
}

func TestCarriedProofRejectsSpecialFile(t *testing.T) {
	a, path := provedAgent(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if a.pathFingerprint(path) != "" || a.carriedProof() != nil {
		t.Fatal("special file acquired a reusable proof fingerprint")
	}
}
