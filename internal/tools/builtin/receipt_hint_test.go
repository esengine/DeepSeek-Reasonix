package builtin

import (
	"strings"
	"testing"
)

// The hint exists so the model can cite something real. Under a deep root every
// entry used to arrive as the same eighty characters of shared prefix — three
// identical strings, no way to tell which file was which. What survives the
// budget has to be the part that differs.
func TestDistinguishKeepsWhatDiffers(t *testing.T) {
	deep := "/private/tmp/claude-501/-Users-yhh-projects-DeepSeek-Reasonix/0cbfc17d-2fbc-4e89-930f-f506537bcc4e/scratchpad/proj/"
	got := distinguish([]string{deep + "store.go", deep + "store_test.go", deep + "cmd/todo/main.go"})

	seen := map[string]bool{}
	for _, g := range got {
		if seen[g] {
			t.Fatalf("two entries render the same: %q", g)
		}
		seen[g] = true
		if len([]rune(g)) > receiptHintWidth {
			t.Errorf("over budget (%d runes): %q", len([]rune(g)), g)
		}
	}
	for i, want := range []string{"store.go", "store_test.go", "main.go"} {
		if !strings.HasSuffix(got[i], want) {
			t.Errorf("lost the end that names the file: %q does not end in %q", got[i], want)
		}
	}
}

// A single item has nothing to share a prefix with, and a short one needs no
// help at all — neither should come back decorated.
func TestDistinguishLeavesShortListsAlone(t *testing.T) {
	got := distinguish([]string{"go test ./..."})
	if len(got) != 1 || got[0] != "go test ./..." {
		t.Errorf("rewrote something that fit: %q", got)
	}
	if got := distinguish([]string{"a/x.go", "b/y.go"}); got[0] != "a/x.go" || got[1] != "b/y.go" {
		t.Errorf("stripped a prefix that was not shared: %q", got)
	}
}

// Cutting at a character boundary would invent a filename that never existed.
func TestCommonDirPrefixStopsAtSeparator(t *testing.T) {
	if got := commonDirPrefix([]string{"/w/store.go", "/w/storefront.go"}); got != "/w/" {
		t.Errorf("commonDirPrefix = %q, want /w/", got)
	}
}
