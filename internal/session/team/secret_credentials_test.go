package team

import (
	"fmt"
	"sync"

	"reasonix/internal/state/teamroster"
)

type testSecretCredentials struct {
	mu     sync.Mutex
	values map[string]string
}

func (c *testSecretCredentials) Save(value string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	slot := fmt.Sprint(len(c.values) + 1)
	c.values[slot] = value
	return slot, nil
}
func (c *testSecretCredentials) Read(slot string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.values[slot]
	if !ok {
		return "", teamroster.ErrSecretCredentials
	}
	return value, nil
}
