package plugin

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// TestHostConcurrentAccess hammers the Host's mutable state from many goroutines:
// writers churn the failures records while readers snapshot status. The mutex
// must keep every read internally consistent; before it, a concurrent slice
// append against a copy could tear the slice header and panic.
func TestHostConcurrentAccess(t *testing.T) {
	h := &Host{}
	// Seed a few "connected" servers so the read paths have data to walk. These
	// methods only read name/transport/toolCount, never the (nil) transport.
	for i := 0; i < 4; i++ {
		h.clients = append(h.clients, &Client{name: fmt.Sprintf("srv-%d", i), transport: "stdio", toolCount: i})
		h.prompts = append(h.prompts, Prompt{Server: fmt.Sprintf("srv-%d", i), Name: "p"})
	}

	const workers = 24
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				switch (w + i) % 6 {
				case 0:
					h.RecordFailure(Spec{Name: fmt.Sprintf("bad-%d", i%8), Type: "stdio"}, errors.New("boom"))
				case 1:
					_ = h.Failures()
				case 2:
					_ = h.Servers()
				case 3:
					_ = h.ServerNames()
				case 4:
					_ = h.has(fmt.Sprintf("srv-%d", i%4))
				case 5:
					_ = h.Prompts()
				}
			}
		}(w)
	}
	wg.Wait()
}
