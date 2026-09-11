//go:build !windows && !darwin

package main

import "github.com/fsnotify/fsnotify"

type fsnotifyWorkspaceWatcher struct {
	watcher *fsnotify.Watcher
}

func newWorkspaceWatcher() (workspaceWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsnotifyWorkspaceWatcher{watcher: w}, nil
}

func (w *fsnotifyWorkspaceWatcher) Events() <-chan fsnotify.Event { return w.watcher.Events }
func (w *fsnotifyWorkspaceWatcher) Errors() <-chan error          { return w.watcher.Errors }
func (w *fsnotifyWorkspaceWatcher) SupportsRecursive() bool       { return false }
func (w *fsnotifyWorkspaceWatcher) Add(path string, _ bool) error { return w.watcher.Add(path) }
func (w *fsnotifyWorkspaceWatcher) Remove(path string) error      { return w.watcher.Remove(path) }
func (w *fsnotifyWorkspaceWatcher) Close() error                  { return w.watcher.Close() }
