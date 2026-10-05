// Package tempdir gives a test a directory whose removal is held to a contract
// Windows can meet.
//
// testing's own TempDir removes once and reports what it finds, which cannot
// tell a leak from teardown still settling: a directory whose children were
// removed can refuse to go, and a name can be in one frame and gone the next
// with nobody removing it. A leak is content that outlives a deadline, so that
// is what this reports, with everything it saw on the way.
package tempdir

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The windows observed in practice close in milliseconds. The bound is what
// keeps a genuinely stuck directory reported rather than waited on.
const (
	quiesceLimit = 2 * time.Second
	quiesceStep  = 20 * time.Millisecond
)

// New returns a directory removed when the test ends, failing the test if it
// does not become removable.
func New(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "reasonix-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := removeDir(dir); err != nil {
			t.Errorf("temp dir: %v", err)
		}
	})
	return dir
}

func removeDir(dir string) error {
	err := os.RemoveAll(dir)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if !teardownRefusal(err) {
		return err
	}
	return quiesce(dir, err, quiesceLimit, os.RemoveAll)
}

// quiesce waits for the directory to go, bounded, and fails with everything it
// saw. No single frame proves anything: a name in it may be one the filesystem
// has yet to drop, and an empty directory may refuse anyway. Content outliving
// the deadline is the leak, and it is named. remove is a parameter so a test
// can pin that decision without racing the filesystem for it.
func quiesce(dir string, first error, limit time.Duration, remove func(string) error) error {
	deadline := time.Now().Add(limit)
	seen := map[string]bool{}
	// Never first: the loop retries before anything reads this, so the value
	// reported is always an error this wait actually observed.
	var last error
	for {
		err := remove(dir)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		last = err
		for _, name := range survivors(dir) {
			seen[name] = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"%s did not become removable within %s: first %w; last %w; holds [%s]; seen while waiting [%s]",
				dir, limit, first, last, strings.Join(survivors(dir), " "), strings.Join(sorted(seen), " "))
		}
		time.Sleep(quiesceStep)
	}
}

func survivors(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func sorted(set map[string]bool) []string {
	out := slices.Sorted(maps.Keys(set))
	return out
}
