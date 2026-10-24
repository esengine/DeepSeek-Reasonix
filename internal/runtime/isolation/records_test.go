package isolation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryEnumerationErrorsAreNotAbsence(t *testing.T) {
	for _, kind := range []string{"result", "integration", "nonregular_integration"} {
		t.Run(kind, func(t *testing.T) {
			repo := gitRepo(t)
			s := NewStore(filepath.Join(t.TempDir(), "[results]"), repo, opened(t, repo))
			path := filepath.Join(s.managedRoot, "unidentified", entryFile)
			if kind != "result" {
				path = filepath.Join(s.integrationRoot(), "int_ABCDEFGHIJKLMNOPQRSTUVWXYZ.json")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			raw := []byte("{")
			if kind == "nonregular_integration" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PreviewRecovery(t.Context()); err == nil {
				t.Fatal("unobservable recovery record was treated as absent")
			}
			if kind != "nonregular_integration" {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, after) {
					t.Fatal("unidentified recovery record changed", err)
				}
			}
		})
	}
}
