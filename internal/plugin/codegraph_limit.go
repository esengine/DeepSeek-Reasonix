package plugin

import (
	"fmt"
	"sync/atomic"
)

// maxCodeGraphInstances caps how many CodeGraph indexer subprocesses may run
// concurrently across the whole process. Each tab/session boots its own plugin
// Host (desktop/tabs.go, boot.go), so opening many tabs on large projects used
// to spawn one full-tree indexer per tab — exhausting file descriptors and
// freezing the machine (#4361, #2992, #3797). A hard cap bounds the blast
// radius: once reached, additional CodeGraph instances refuse to start and the
// agent degrades to grep/glob, which is the documented manual workaround
// (`[codegraph] enabled = false`) applied automatically instead of a crash.
const maxCodeGraphInstances = 4

var liveCodeGraphInstances atomic.Int32

// acquireCodeGraphSlot reserves one of the bounded CodeGraph instance slots.
// It returns a release func (idempotent) and an error when the cap is reached.
// The cap only governs CodeGraph; every other stdio plugin is unaffected.
func acquireCodeGraphSlot() (func(), error) {
	n := liveCodeGraphInstances.Add(1)
	if n > maxCodeGraphInstances {
		liveCodeGraphInstances.Add(-1)
		return nil, fmt.Errorf("codegraph: %d instances already running (cap %d); not starting another to avoid file-descriptor exhaustion — close some tabs/sessions or set codegraph off for this one", n-1, maxCodeGraphInstances)
	}
	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			liveCodeGraphInstances.Add(-1)
		}
	}, nil
}
