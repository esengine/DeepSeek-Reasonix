package workspacelist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
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
