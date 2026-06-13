package plugin

import (
	"context"
	"testing"
)

func TestCachedShellPATHMemoizesNonEmpty(t *testing.T) {
	calls := 0
	probe := cachedShellPATH(func(context.Context) string {
		calls++
		return "/opt/homebrew/bin"
	})
	for range 3 {
		if got := probe(context.Background()); got != "/opt/homebrew/bin" {
			t.Fatalf("probe() = %q", got)
		}
	}
	if calls != 1 {
		t.Errorf("probe ran %d times, want 1 (memoized)", calls)
	}
}

func TestCachedShellPATHRetriesAfterEmpty(t *testing.T) {
	calls := 0
	results := []string{"", "", "/usr/local/bin"}
	probe := cachedShellPATH(func(context.Context) string {
		r := results[calls]
		calls++
		return r
	})
	// Empty results are not cached, so the probe keeps trying; once it returns
	// a non-empty PATH that value is memoized.
	want := []string{"", "", "/usr/local/bin", "/usr/local/bin"}
	for i, w := range want {
		if got := probe(context.Background()); got != w {
			t.Fatalf("call %d: probe() = %q, want %q", i, got, w)
		}
	}
	if calls != 3 {
		t.Errorf("probe ran %d times, want 3", calls)
	}
}
