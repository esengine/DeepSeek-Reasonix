package provider

import (
	"fmt"
	"sync"
	"testing"
)

type registryRaceProvider struct{ Provider }

// Parallel Build tests register their own kinds while other sessions resolve
// theirs; run under -race, any unguarded access to the registry fails here.
func TestRegistryToleratesConcurrentRegisterAndLookup(t *testing.T) {
	const writers = 8
	var wg sync.WaitGroup
	for i := range writers {
		kind := fmt.Sprintf("registry-race-%s-%d", t.Name(), i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			Register(kind, func(Config) (Provider, error) { return registryRaceProvider{}, nil })
		}()
		go func() {
			defer wg.Done()
			for range 200 {
				_, _ = New(kind, Config{})
				_ = Kinds()
			}
		}()
	}
	wg.Wait()
	for i := range writers {
		if _, err := New(fmt.Sprintf("registry-race-%s-%d", t.Name(), i), Config{}); err != nil {
			t.Fatalf("kind %d not resolvable after registration: %v", i, err)
		}
	}
}
