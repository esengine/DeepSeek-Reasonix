package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A directory another test creates and removes while the guard walks the tree
// must not fail the walk, and a join in a stable file is still found.
func TestStateRootJoinsSurvivesDirectoriesVanishingMidWalk(t *testing.T) {
	root := t.TempDir()
	src := "package x\n\nfunc f() { _ = filepath.Join(userSupportDir(), \"kept\") }\n"
	if err := os.WriteFile(filepath.Join(root, "kept.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			dir := filepath.Join(root, fmt.Sprintf("churn-%d", i%64))
			_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
			_ = os.RemoveAll(dir)
		}
	})
	defer func() { close(stop); wg.Wait() }()
	for range 3000 {
		found, err := stateRootJoins(root)
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if found["kept"] == "" {
			t.Fatalf("join in a stable file was lost: %v", found)
		}
	}
}
