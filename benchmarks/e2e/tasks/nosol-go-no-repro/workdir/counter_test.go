package counter

import (
	"sync"
	"testing"
)

func TestConcurrentAdd(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				c.Add("hit")
			}
		}()
	}
	wg.Wait()
	if got := c.Get("hit"); got != 32000 {
		t.Fatalf("got %d, want 32000", got)
	}
}
