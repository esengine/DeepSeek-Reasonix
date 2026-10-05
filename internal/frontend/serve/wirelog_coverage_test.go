package serve

import (
	"testing"

	"reasonix/internal/contract/eventwire"
)

// A frame's absence from the log is a decision, and the only way to tell a
// decision from an oversight is to make every kind name one list or the other.
// The plan verdict was emitted by the kernel and missing from every replay
// because nothing forced that choice when the kind was added.
func TestEveryEventKindIsEitherLoggedOrDeliberatelySkipped(t *testing.T) {
	names := eventwire.KindNames()
	if len(names) == 0 {
		t.Fatal("no wire kinds; the guard would pass vacuously")
	}
	for _, name := range names {
		switch {
		case wireLogKinds[name] && wireLogSkipped[name]:
			t.Errorf("%q is on both lists", name)
		case !wireLogKinds[name] && !wireLogSkipped[name]:
			t.Errorf("%q is on neither list; decide whether a replay needs it and say so above", name)
		}
	}
	known := map[string]bool{}
	for _, name := range names {
		known[name] = true
	}
	for _, list := range []map[string]bool{wireLogKinds, wireLogSkipped} {
		for name := range list {
			if !known[name] {
				t.Errorf("the lists name %q, which is not a wire kind", name)
			}
		}
	}
}
