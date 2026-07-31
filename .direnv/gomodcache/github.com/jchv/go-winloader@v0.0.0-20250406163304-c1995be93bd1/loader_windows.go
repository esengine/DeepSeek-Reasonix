package winloader

import (
	"github.com/jchv/go-winloader/internal/memloader"
	"github.com/jchv/go-winloader/internal/winloader"
)

var native = winloader.Loader{}

var cache = memloader.NewCache(native)

var ldr = memloader.New(memloader.Options{
	Next:    cache,
	Machine: winloader.NativeMachine{},
})

// LoadFromFile loads a Windows module from file using the native Windows
// loader.
func LoadFromFile(name string) (Module, error) {
	return native.Load(name)
}

// LoadFromMemory loads a Windows module from memory.
func LoadFromMemory(data []byte) (Module, error) {
	return ldr.LoadMem(data)
}

// AddToCache adds a module to the loader cache, allowing in-memory libraries
// to link to it. Note that modules in the cache must exist in the same
// address space.
func AddToCache(name string, module Module) error {
	return cache.Add(name, module)
}
