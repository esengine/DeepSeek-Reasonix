package main

import "github.com/fsnotify/fsnotify"

// workspaceWatcher isolates the platform-specific directory notification
// backend from revision coalescing and workspace lifecycle ownership.
type workspaceWatcher interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	SupportsRecursive() bool
	Add(path string, recursive bool) error
	Remove(path string) error
	Close() error
}
