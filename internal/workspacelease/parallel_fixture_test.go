package workspacelease

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func unrelatedTreePath(t *testing.T, blocker *Owner, base string) (string, string) {
	t.Helper()
	blockedSlot := blocker.treeLockPath(blocker.canonical)
	for i := 1; i < treeLockStripes*2; i++ {
		root := filepath.Join(base, fmt.Sprintf("workspace-%d", i))
		path := filepath.Join(root, "b.go")
		rootKey, _, err := canonicalFileKey(root)
		if err != nil {
			t.Fatal(err)
		}
		pathKey, _, err := canonicalFileKey(path)
		if err != nil {
			t.Fatal(err)
		}
		if blocker.treeLockPath(rootKey) == blockedSlot || blocker.treeLockPath(pathKey) == blockedSlot {
			continue
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		return root, path
	}
	t.Fatal("could not find unrelated hierarchy lock slots")
	return "", ""
}

func distinctPathSlotInDirectory(t *testing.T, owner *Owner, dir, blockedSlot string) string {
	t.Helper()
	for i := range pathLockStripes * 2 {
		path := filepath.Join(dir, fmt.Sprintf("parallel-%d.go", i))
		if canonicalPathSlot(t, owner, path) != blockedSlot {
			return path
		}
	}
	t.Fatal("could not find a distinct path stripe in the requested directory")
	return ""
}
