package taskcatalog

import "sync"

func (c *Catalog) lockProject(projectKey string) func() {
	value, _ := c.projectLocks.LoadOrStore(projectKey, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
