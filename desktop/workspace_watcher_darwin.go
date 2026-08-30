//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework CoreServices -framework CoreFoundation

#include <CoreServices/CoreServices.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct reasonix_fsevents_subscription reasonix_fsevents_subscription;

reasonix_fsevents_subscription *reasonix_fsevents_start(
	const char *path,
	uintptr_t token,
	double latency,
	int *error_code
);
void reasonix_fsevents_stop(reasonix_fsevents_subscription *subscription);
*/
import "C"

import (
	"fmt"
	"path/filepath"
	"runtime/cgo"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/fsnotify/fsnotify"
)

const (
	darwinWorkspaceEventBuffer = 1024
	darwinWorkspaceLatency     = 0.050

	darwinFSEventMustScanSubDirs = uint32(C.kFSEventStreamEventFlagMustScanSubDirs)
	darwinFSEventUserDropped     = uint32(C.kFSEventStreamEventFlagUserDropped)
	darwinFSEventKernelDropped   = uint32(C.kFSEventStreamEventFlagKernelDropped)
	darwinFSEventEventIDsWrapped = uint32(C.kFSEventStreamEventFlagEventIdsWrapped)
	darwinFSEventHistoryDone     = uint32(C.kFSEventStreamEventFlagHistoryDone)
	darwinFSEventRootChanged     = uint32(C.kFSEventStreamEventFlagRootChanged)
	darwinFSEventMount           = uint32(C.kFSEventStreamEventFlagMount)
	darwinFSEventUnmount         = uint32(C.kFSEventStreamEventFlagUnmount)
	darwinFSEventItemCreated     = uint32(C.kFSEventStreamEventFlagItemCreated)
	darwinFSEventItemRemoved     = uint32(C.kFSEventStreamEventFlagItemRemoved)
	darwinFSEventItemInodeMeta   = uint32(C.kFSEventStreamEventFlagItemInodeMetaMod)
	darwinFSEventItemRenamed     = uint32(C.kFSEventStreamEventFlagItemRenamed)
	darwinFSEventItemModified    = uint32(C.kFSEventStreamEventFlagItemModified)
	darwinFSEventItemFinderInfo  = uint32(C.kFSEventStreamEventFlagItemFinderInfoMod)
	darwinFSEventItemChangeOwner = uint32(C.kFSEventStreamEventFlagItemChangeOwner)
	darwinFSEventItemXattr       = uint32(C.kFSEventStreamEventFlagItemXattrMod)
)

type darwinWorkspaceWatcher struct {
	mu         sync.Mutex
	events     chan fsnotify.Event
	errors     chan error
	watches    map[string]*darwinWorkspaceSubscription
	isClosed   bool
	closed     atomic.Bool
	overflowed atomic.Bool
	stopWG     sync.WaitGroup
	closeDone  chan struct{}
}

type darwinWorkspaceSubscription struct {
	watcher    *darwinWorkspaceWatcher
	path       string
	recursive  bool
	native     *C.reasonix_fsevents_subscription
	handle     cgo.Handle
	stopOnce   sync.Once
	stopNative func()
}

func newWorkspaceWatcher() (workspaceWatcher, error) {
	return &darwinWorkspaceWatcher{
		events:    make(chan fsnotify.Event, darwinWorkspaceEventBuffer),
		errors:    make(chan error, 1),
		watches:   make(map[string]*darwinWorkspaceSubscription),
		closeDone: make(chan struct{}),
	}, nil
}

func (w *darwinWorkspaceWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w *darwinWorkspaceWatcher) Errors() <-chan error          { return w.errors }
func (w *darwinWorkspaceWatcher) SupportsRecursive() bool       { return true }

func (w *darwinWorkspaceWatcher) Add(path string, recursive bool) error {
	path = canonicalWorkspaceRoot(path)
	if path == "" {
		return fmt.Errorf("start FSEvents stream: empty path")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.isClosed {
		return fsnotify.ErrClosed
	}
	if _, exists := w.watches[path]; exists {
		return nil
	}

	sub := &darwinWorkspaceSubscription{watcher: w, path: path, recursive: recursive}
	sub.handle = cgo.NewHandle(sub)
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var errorCode C.int
	sub.native = C.reasonix_fsevents_start(cPath, C.uintptr_t(sub.handle), C.double(darwinWorkspaceLatency), &errorCode)
	if sub.native == nil {
		sub.handle.Delete()
		return fmt.Errorf("start FSEvents stream for %q: %s", path, darwinFSEventsStartError(int(errorCode)))
	}
	w.watches[path] = sub
	return nil
}

func (w *darwinWorkspaceWatcher) Remove(path string) error {
	path = canonicalWorkspaceRoot(path)
	w.mu.Lock()
	sub := w.watches[path]
	delete(w.watches, path)
	if sub != nil {
		w.stopWG.Add(1)
	}
	w.mu.Unlock()
	if sub != nil {
		sub.stop()
		w.stopWG.Done()
	}
	return nil
}

func (w *darwinWorkspaceWatcher) Close() error {
	w.mu.Lock()
	if w.isClosed {
		done := w.closeDone
		w.mu.Unlock()
		<-done
		return nil
	}
	w.isClosed = true
	w.closed.Store(true)
	subs := make([]*darwinWorkspaceSubscription, 0, len(w.watches))
	for _, sub := range w.watches {
		subs = append(subs, sub)
	}
	w.watches = make(map[string]*darwinWorkspaceSubscription)
	w.mu.Unlock()

	for _, sub := range subs {
		sub.stop()
	}
	w.stopWG.Wait()
	close(w.events)
	close(w.errors)
	close(w.closeDone)
	return nil
}

func (s *darwinWorkspaceSubscription) stop() {
	s.stopOnce.Do(func() {
		if s.stopNative != nil {
			s.stopNative()
			return
		}
		C.reasonix_fsevents_stop(s.native)
		s.native = nil
		s.handle.Delete()
	})
}

//export reasonixFSEventsEvent
func reasonixFSEventsEvent(token C.uintptr_t, eventPath *C.char, eventFlags C.uint32_t) {
	if eventPath == nil {
		return
	}
	value := cgo.Handle(token).Value()
	sub, ok := value.(*darwinWorkspaceSubscription)
	if !ok || sub == nil {
		return
	}
	sub.publish(filepath.Clean(C.GoString(eventPath)), uint32(eventFlags))
}

func (s *darwinWorkspaceSubscription) publish(path string, flags uint32) {
	if path == "" || s.watcher.closed.Load() || !s.accepts(path) {
		return
	}
	op, overflow := darwinWorkspaceEvent(flags)
	if op != 0 {
		s.watcher.sendEvent(fsnotify.Event{Name: path, Op: op})
	}
	if overflow {
		s.watcher.sendOverflow()
	}
}

func (s *darwinWorkspaceSubscription) accepts(path string) bool {
	rel, err := filepath.Rel(s.path, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return s.recursive || rel == "." || !strings.ContainsRune(rel, filepath.Separator)
}

func (w *darwinWorkspaceWatcher) sendEvent(event fsnotify.Event) {
	if w.closed.Load() {
		return
	}
	select {
	case w.events <- event:
		w.overflowed.Store(false)
	default:
		w.sendOverflow()
	}
}

func (w *darwinWorkspaceWatcher) sendOverflow() {
	if w.closed.Load() || w.overflowed.Swap(true) {
		return
	}
	select {
	case w.errors <- fsnotify.ErrEventOverflow:
	default:
	}
}

func darwinWorkspaceEvent(flags uint32) (fsnotify.Op, bool) {
	var op fsnotify.Op
	if flags&darwinFSEventItemRemoved != 0 {
		op |= fsnotify.Remove
	}
	if flags&darwinFSEventItemRenamed != 0 {
		op |= fsnotify.Rename
	}
	if flags&darwinFSEventItemCreated != 0 {
		op |= fsnotify.Create
	}
	const writeFlags = darwinFSEventItemModified |
		darwinFSEventItemInodeMeta |
		darwinFSEventItemFinderInfo |
		darwinFSEventItemXattr |
		darwinFSEventItemChangeOwner
	if flags&writeFlags != 0 {
		op |= fsnotify.Write
	}

	overflowFlags := darwinFSEventMustScanSubDirs |
		darwinFSEventUserDropped |
		darwinFSEventKernelDropped |
		darwinFSEventEventIDsWrapped |
		darwinFSEventMount |
		darwinFSEventUnmount
	overflow := flags&overflowFlags != 0
	if flags&darwinFSEventRootChanged != 0 {
		op |= fsnotify.Rename
		overflow = true
	}
	return op, overflow
}

func darwinFSEventsStartError(code int) string {
	switch code {
	case 1:
		return "invalid filesystem path"
	case 2:
		return "allocation failed"
	case 3:
		return "dispatch queue creation failed"
	case 4:
		return "FSEventStream creation failed"
	case 5:
		return "FSEventStream start failed"
	default:
		return fmt.Sprintf("native error %d", code)
	}
}
