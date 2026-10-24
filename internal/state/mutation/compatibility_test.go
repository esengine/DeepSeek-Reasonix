package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnknownOrCorruptCoordinationRecordIsPreserved(t *testing.T) {
	for _, raw := range []string{`{"version":2,"id":"future","order":1,"paths":["/future"]}`, `{"version":`} {
		t.Run(raw, func(t *testing.T) {
			root, state := t.TempDir(), t.TempDir()
			c := testCoordinator(t, root, state)
			path := c.recordPath("future")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Acquire(t.Context(), intent(filepath.Join(root, "file"))); !errors.Is(err, ErrProtocol) {
				t.Fatalf("acquire: %v", err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != raw {
				t.Fatalf("unknown record modified: %q %v", got, err)
			}
		})
	}
}
