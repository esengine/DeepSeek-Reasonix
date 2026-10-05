package counter

// Counter tallies events by name.
type Counter struct {
	counts map[string]int
}

func New() *Counter {
	return &Counter{counts: map[string]int{}}
}

// Add records one occurrence of name.
func (c *Counter) Add(name string) {
	c.counts[name]++
}

// Get returns how many times name was recorded.
func (c *Counter) Get(name string) int {
	return c.counts[name]
}
