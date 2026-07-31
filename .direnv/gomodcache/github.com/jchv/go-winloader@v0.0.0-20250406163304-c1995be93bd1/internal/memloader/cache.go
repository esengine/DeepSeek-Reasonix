package memloader

import (
	"strings"

	"github.com/jchv/go-winloader/internal/loader"
)

// Cache implements a memory cache for PE modules.
type Cache struct {
	next  loader.Loader
	cache map[string]loader.Module
}

// NewCache creates a new cache with the specified options.
func NewCache(next loader.Loader) *Cache {
	return &Cache{
		next:  next,
		cache: make(map[string]loader.Module),
	}
}

// Load implements loader.Loader by loading from cache or falling back.
func (c *Cache) Load(libname string) (loader.Module, error) {
	if m, ok := c.cache[strings.ToLower(libname)]; ok {
		return m, nil
	}
	if m, ok := c.cache[strings.ToLower(libname)+".dll"]; ok {
		return m, nil
	}
	return c.next.Load(libname)
}

// Add adds a module to the cache.
func (c *Cache) Add(libname string, m loader.Module) error {
	libname = strings.ToLower(libname)
	if strings.HasSuffix(libname, ".dll") {
		libname = libname[0 : len(libname)-4]
	}
	c.cache[libname] = m
	return nil
}
