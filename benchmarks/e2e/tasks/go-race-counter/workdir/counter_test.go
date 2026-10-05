package counter

import (
	"sync"
	"testing"
)

func TestAddConcurrently(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Add("hit")
			}
		}()
	}
	wg.Wait()
	if got := c.Get("hit"); got != 5000 {
		t.Fatalf("got %d, want 5000", got)
	}
}
