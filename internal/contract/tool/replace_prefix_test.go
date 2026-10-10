package tool

import (
	"strings"
	"sync"
	"testing"
)

// The window a remove-then-add pair leaves open is what this closes: a request
// composed inside it would be built from a server with no tools at all, which
// is both a moved cache prefix and a call the model was offered resolving to
// nothing.
func TestReplacePrefixNeverShowsAnEmptyNamespace(t *testing.T) {
	r := NewRegistry()
	first := []Tool{stubTool{name: "mcp__atlas__a"}, stubTool{name: "mcp__atlas__b"}}
	second := []Tool{stubTool{name: "mcp__atlas__b"}, stubTool{name: "mcp__atlas__c"}}
	r.ReplacePrefix("mcp__atlas__", first)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	lowest := 2
	var mu sync.Mutex
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				seen := 0
				for _, s := range r.Schemas() {
					if strings.HasPrefix(s.Name, "mcp__atlas__") {
						seen++
					}
				}
				mu.Lock()
				lowest = min(lowest, seen)
				mu.Unlock()
			}
		})
	}
	for i := range 200 {
		if i%2 == 0 {
			r.ReplacePrefix("mcp__atlas__", second)
			continue
		}
		r.ReplacePrefix("mcp__atlas__", first)
	}
	close(stop)
	wg.Wait()

	if lowest < 2 {
		t.Fatalf("a reader saw %d tools under the prefix mid-swap, want never fewer than 2", lowest)
	}
}

// Suspension is the user turning a server off. A swap is not a way back in.
func TestReplacePrefixLeavesASuspendedPrefixOff(t *testing.T) {
	r := NewRegistry()
	r.SuspendPrefix("mcp__atlas__")
	if got := r.ReplacePrefix("mcp__atlas__", []Tool{stubTool{name: "mcp__atlas__a"}}); got != 0 {
		t.Fatalf("registered %d tools into a suspended prefix, want 0", got)
	}
	if _, ok := r.Get("mcp__atlas__a"); ok {
		t.Fatal("a swap registered a tool the session had turned off")
	}
}

// The admit ceiling is what a restricted registry may ever hold. No
// registration path may widen it, this one included.
func TestReplacePrefixHonorsTheAdmitCeiling(t *testing.T) {
	r := NewRegistry()
	r.Restrict(func(tl Tool) bool { return tl.Name() != "mcp__atlas__b" })
	r.ReplacePrefix("mcp__atlas__", []Tool{stubTool{name: "mcp__atlas__a"}, stubTool{name: "mcp__atlas__b"}})

	if _, ok := r.Get("mcp__atlas__a"); !ok {
		t.Fatal("an admitted tool did not reach the registry")
	}
	if _, ok := r.Get("mcp__atlas__b"); ok {
		t.Fatal("a swap admitted a tool the ceiling rejects")
	}
}

// The namespace is what the call owns: a tool from somewhere else would be
// registered here and then removed by the next swap of a prefix it never had.
func TestReplacePrefixIgnoresToolsOutsideThePrefix(t *testing.T) {
	r := NewRegistry()
	r.Add(stubTool{name: "mcp__ledger__post"})
	r.ReplacePrefix("mcp__atlas__", []Tool{stubTool{name: "mcp__ledger__other"}})

	if _, ok := r.Get("mcp__ledger__other"); ok {
		t.Fatal("a swap registered a tool outside the prefix it owns")
	}
	if _, ok := r.Get("mcp__ledger__post"); !ok {
		t.Fatal("a swap removed a tool outside the prefix it owns")
	}
}
