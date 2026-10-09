package workspacelist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadKeepsWindowsPathsIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	put(t, path, `{"paths":["D:\\work\\app"," C:\\Users\\_\\x ",""],"launch":"C:\\Users\\_\\x"}`)
	list, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Paths) != 2 || list.Paths[0] != `D:\work\app` || list.Paths[1] != `C:\Users\_\x` || list.Launch != `C:\Users\_\x` {
		t.Fatalf("list = %+v", list)
	}
}

func TestMergeOnceAppendsAndSetsAside(t *testing.T) {
	dir := t.TempDir()
	cur, old := filepath.Join(dir, "cur.json"), filepath.Join(dir, "old.json")
	put(t, cur, `{"paths":["a","b"],"launch":"b"}`)
	put(t, old, `["b","c"]`)
	n, err := MergeOnce(context.Background(), cur, old)
	if err != nil || n != 1 {
		t.Fatalf("merge = %d, %v", n, err)
	}
	list, _ := Read(cur)
	if len(list.Paths) != 3 || list.Paths[2] != "c" || list.Launch != "b" {
		t.Fatalf("list = %+v", list)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old list not set aside")
	}
	if n, err := MergeOnce(context.Background(), cur, old); n != 0 || err != nil {
		t.Fatalf("second merge = %d, %v", n, err)
	}
}

func TestMergeOnceIntoAMissingListCreatesIt(t *testing.T) {
	dir := t.TempDir()
	cur, old := filepath.Join(dir, "cur.json"), filepath.Join(dir, "old.json")
	put(t, old, `{"paths":["x"],"launch":"x"}`)
	if _, err := MergeOnce(context.Background(), cur, old); err != nil {
		t.Fatal(err)
	}
	if list, _ := Read(cur); len(list.Paths) != 1 || list.Launch != "x" {
		t.Fatalf("list = %+v", list)
	}
}

func TestMergeOnceRefusesAnUnreadableOldListAndKeepsIt(t *testing.T) {
	dir := t.TempDir()
	cur, old := filepath.Join(dir, "cur.json"), filepath.Join(dir, "old.json")
	put(t, cur, `{"paths":["a"]}`)
	put(t, old, `{broken`)
	if _, err := MergeOnce(context.Background(), cur, old); err == nil {
		t.Fatal("a corrupt list merged without an error")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("a list that could not be read was set aside")
	}
}

// A merge racing ordinary updates must never leave the list half written or
// drop an entry an update added.
func TestMergeOnceRacesUpdatesWithoutLosingEntries(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "cur.json")
	put(t, cur, `{"paths":["seed"]}`)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			_ = Update(context.Background(), cur, true, func(l *List) error {
				l.Paths = append(l.Paths, fmt.Sprintf("u%d", i))
				return nil
			})
		})
		wg.Go(func() {
			old := filepath.Join(dir, fmt.Sprintf("old%d.json", i))
			put(t, old, fmt.Sprintf(`["m%d"]`, i))
			_, _ = MergeOnce(context.Background(), cur, old)
		})
	}
	wg.Wait()
	list, err := Read(cur)
	if err != nil {
		t.Fatalf("list unreadable after the race: %v", err)
	}
	if len(list.Paths) != 17 {
		t.Fatalf("got %d paths %v, want 17", len(list.Paths), list.Paths)
	}
}

func TestUpdateWaitsAsLongAsItsCallerAllows(t *testing.T) {
	prev := defaultLockWait
	defaultLockWait = 50 * time.Millisecond
	t.Cleanup(func() { defaultLockWait = prev })
	path := filepath.Join(t.TempDir(), FileName)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const writers = 6
	errs := make(chan error, writers)
	for range writers {
		go func() {
			errs <- Update(ctx, path, false, func(l *List) error {
				time.Sleep(30 * time.Millisecond)
				l.Paths = append(l.Paths, "p")
				return nil
			})
		}()
	}
	for range writers {
		if err := <-errs; err != nil {
			t.Fatalf("a writer queued inside its caller's deadline gave up: %v", err)
		}
	}
}

func TestUpdateWithoutADeadlineStillGivesUp(t *testing.T) {
	prev := defaultLockWait
	defaultLockWait = 50 * time.Millisecond
	t.Cleanup(func() { defaultLockWait = prev })
	path := filepath.Join(t.TempDir(), FileName)
	release := make(chan struct{})
	held := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Update(context.Background(), path, false, func(*List) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer func() { close(release); <-done }()
	err := Update(context.Background(), path, false, func(*List) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended update without a deadline = %v, want deadline exceeded", err)
	}
}
