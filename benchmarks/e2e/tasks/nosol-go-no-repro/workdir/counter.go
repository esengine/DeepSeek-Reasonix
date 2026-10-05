package counter

import "sync"

// Counter tallies events by name. Every field access is guarded; the zero
// value is not usable, so New is the only constructor.
type Counter struct {
	mu     sync.Mutex
	counts map[string]int
}

func New() *Counter {
	return &Counter{counts: map[string]int{}}
}

// Add records one occurrence of name.
func (c *Counter) Add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name]++
}

// Get returns how many times name was recorded.
func (c *Counter) Get(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}
