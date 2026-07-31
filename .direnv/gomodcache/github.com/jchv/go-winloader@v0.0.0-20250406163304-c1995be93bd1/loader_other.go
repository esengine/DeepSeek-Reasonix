//go:build !windows
// +build !windows

package winloader

import "fmt"

// LoadFromFile loads a Windows module from file using the native Windows
// loader.
func LoadFromFile(name string) (Module, error) {
	return nil, fmt.Errorf("unsupported platform")
}

// LoadFromMemory loads a Windows module from memory.
func LoadFromMemory(data []byte) (Module, error) {
	return nil, fmt.Errorf("unsupported platform")
}

// AddToCache adds a module to the loader cache, allowing in-memory libraries
// to link to it. Note that modules in the cache must exist in the same
// address space.
func AddToCache(name string, module Module) error {
	return nil
}
