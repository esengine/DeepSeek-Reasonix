package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/memory"
)

func TestMemoryCommandsReportDirectoryReadFailure(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "memory")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Memory: &memory.Set{Store: memory.Store{Dir: path}}})
	t.Cleanup(c.Close)
	for _, command := range []string{"/memory", "/memory subjects", "/memory archived", "/memory revisions fact", "/memory pin fact", "/memory verify fact", "/memory restore fact 1"} {
		t.Run(command, func(t *testing.T) {
			if text := MemoryCommandText(c, command); !strings.Contains(text, "read memory directory") {
				t.Fatalf("command returned %q instead of a directory read error", text)
			}
		})
	}
	revisions, err := c.MemoryRevisions("fact")
	var directory *memory.DirectoryReadError
	if revisions != nil || !errors.As(err, &directory) || directory.Path != path {
		t.Fatalf("MemoryRevisions = %v, %v", revisions, err)
	}
}
