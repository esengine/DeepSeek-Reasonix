package pluginpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
)

// TestStateConcurrentUpsertAndSetEnabled pins that concurrent load-modify-save
// cycles on the state file don't clobber each other: every plugin upserted by a
// racing goroutine must survive, with the enabled flag it was last given.
func TestStateConcurrentUpsertAndSetEnabled(t *testing.T) {
	home := t.TempDir()
	const n = 16

	var wg sync.WaitGroup
	for i := range n {
		name := fmt.Sprintf("plugin-%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Upsert(home, InstalledPlugin{Name: name, Root: "plugins/" + name}); err != nil {
				t.Errorf("Upsert(%s): %v", name, err)
				return
			}
			if err := SetEnabled(home, name, true); err != nil {
				t.Errorf("SetEnabled(%s): %v", name, err)
			}
		}()
	}
	wg.Wait()

	st, err := LoadState(home)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(st.Plugins) != n {
		t.Fatalf("got %d plugins, want %d (lost updates)", len(st.Plugins), n)
	}
	for _, p := range st.Plugins {
		if !p.Enabled {
			t.Errorf("plugin %s lost its SetEnabled update", p.Name)
		}
	}
}

// TestStateConcurrentRemove pins that racing removals each observe their own
// plugin exactly once and leave nothing behind.
func TestStateConcurrentRemove(t *testing.T) {
	home := t.TempDir()
	const n = 8
	for i := range n {
		name := fmt.Sprintf("plugin-%02d", i)
		if err := Upsert(home, InstalledPlugin{Name: name, Root: "plugins/" + name}); err != nil {
			t.Fatalf("Upsert(%s): %v", name, err)
		}
	}

	var wg sync.WaitGroup
	for i := range n {
		name := fmt.Sprintf("plugin-%02d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			removed, ok, err := Remove(home, name)
			if err != nil {
				t.Errorf("Remove(%s): %v", name, err)
				return
			}
			if !ok || removed.Name != name {
				t.Errorf("Remove(%s) = %+v, ok=%v", name, removed, ok)
			}
		}()
	}
	wg.Wait()

	st, err := LoadState(home)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(st.Plugins) != 0 {
		t.Fatalf("got %d plugins after removing all, want 0", len(st.Plugins))
	}
}

// TestStateLoadDuringSaveNeverSeesTornFile pins the atomic write: a reader
// racing a writer sees either the old state or the new one, never a truncated
// or half-written file (which would surface as a JSON parse error). On Windows
// the rename that publishes a new state file can make a concurrent open fail
// with a transient sharing violation — that is the platform's locking
// behavior, not a torn file, so such reads are retried instead of failed.
func TestStateLoadDuringSaveNeverSeesTornFile(t *testing.T) {
	home := t.TempDir()
	if err := Upsert(home, InstalledPlugin{Name: "seed", Root: "plugins/seed", Enabled: true}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100 {
			if err := SetEnabled(home, "seed", i%2 == 0); err != nil {
				t.Errorf("SetEnabled: %v", err)
				return
			}
		}
	}()
	// Keep the writer's lifetime inside the test body: a t.Fatalf below must
	// not let TempDir cleanup race the still-running writer goroutine.
	defer func() { <-done }()

	for {
		st, err := LoadState(home)
		if err != nil {
			var jsonErr *json.SyntaxError
			if errors.As(err, &jsonErr) {
				t.Fatalf("LoadState saw a torn state file: %v", err)
			}
			if runtime.GOOS == "windows" {
				// Transient sharing violation while the writer renames the
				// new state into place — retry, it is not a torn file.
				continue
			}
			t.Fatalf("LoadState: %v", err)
		}
		if len(st.Plugins) != 1 || st.Plugins[0].Name != "seed" {
			t.Fatalf("state = %+v, want the single seed plugin", st.Plugins)
		}
		select {
		case <-done:
			return
		default:
		}
	}
}
