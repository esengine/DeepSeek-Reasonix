//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows"
)

const (
	windowsWorkspaceWatchBuffer = 64 * 1024
	windowsWorkspaceEventBuffer = 256
)

// Windows keeps a single recursive ReadDirectoryChangesW handle per root.
// Per-directory handles can prevent renaming an ancestor while a read is
// pending, even when each handle was opened with FILE_SHARE_DELETE (#8770).
type windowsWorkspaceWatcher struct {
	mu       sync.Mutex
	events   chan fsnotify.Event
	errors   chan error
	closed   chan struct{}
	watches  map[string]*windowsWorkspaceSubscription
	isClosed bool
	wg       sync.WaitGroup
}

type windowsWorkspaceSubscription struct {
	path      string
	handle    windows.Handle
	event     windows.Handle
	stopEvent windows.Handle
	recursive bool
	ready     chan error
	stop      chan struct{}
	done      chan struct{}
	readyOnce sync.Once
	stopOnce  sync.Once
	stopErr   error
}

func newWorkspaceWatcher() (workspaceWatcher, error) {
	return &windowsWorkspaceWatcher{
		events:  make(chan fsnotify.Event, windowsWorkspaceEventBuffer),
		errors:  make(chan error, 1),
		closed:  make(chan struct{}),
		watches: make(map[string]*windowsWorkspaceSubscription),
	}, nil
}

func (w *windowsWorkspaceWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w *windowsWorkspaceWatcher) Errors() <-chan error          { return w.errors }
func (w *windowsWorkspaceWatcher) SupportsRecursive() bool       { return true }

func (w *windowsWorkspaceWatcher) Add(path string, recursive bool) error {
	path = filepath.Clean(path)
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return os.NewSyscallError("CreateFile", err)
	}
	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return os.NewSyscallError("CreateEvent", err)
	}
	stopEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(event)
		return os.NewSyscallError("CreateEvent(stop)", err)
	}

	key := windowsWorkspaceWatchKey(path)
	w.mu.Lock()
	if w.isClosed {
		w.mu.Unlock()
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(event)
		_ = windows.CloseHandle(stopEvent)
		return fsnotify.ErrClosed
	}
	if _, exists := w.watches[key]; exists {
		w.mu.Unlock()
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(event)
		_ = windows.CloseHandle(stopEvent)
		return nil
	}
	sub := &windowsWorkspaceSubscription{
		path: path, handle: handle, event: event, stopEvent: stopEvent, recursive: recursive,
		ready: make(chan error, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
	w.watches[key] = sub
	w.wg.Add(1)
	w.mu.Unlock()
	go w.read(sub)
	if err := <-sub.ready; err != nil {
		w.mu.Lock()
		if w.watches[key] == sub {
			delete(w.watches, key)
		}
		w.mu.Unlock()
		_ = sub.close()
		return err
	}
	return nil
}

func (w *windowsWorkspaceWatcher) Remove(path string) error {
	key := windowsWorkspaceWatchKey(path)
	w.mu.Lock()
	sub := w.watches[key]
	delete(w.watches, key)
	w.mu.Unlock()
	if sub == nil {
		return nil
	}
	return sub.close()
}

func (w *windowsWorkspaceWatcher) Close() error {
	w.mu.Lock()
	if w.isClosed {
		w.mu.Unlock()
		return nil
	}
	w.isClosed = true
	close(w.closed)
	subs := make([]*windowsWorkspaceSubscription, 0, len(w.watches))
	for _, sub := range w.watches {
		subs = append(subs, sub)
	}
	w.watches = make(map[string]*windowsWorkspaceSubscription)
	w.mu.Unlock()

	var firstErr error
	for _, sub := range subs {
		if err := sub.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	w.wg.Wait()
	close(w.events)
	close(w.errors)
	return firstErr
}

func (s *windowsWorkspaceSubscription) close() error {
	s.stopOnce.Do(func() {
		close(s.stop)
		if err := windows.SetEvent(s.stopEvent); err != nil {
			s.stopErr = os.NewSyscallError("SetEvent(stop)", err)
			_ = windows.CancelIoEx(s.handle, nil)
		}
		<-s.done
		if err := windows.CloseHandle(s.handle); err != nil && s.stopErr == nil {
			s.stopErr = os.NewSyscallError("CloseHandle", err)
		}
		if err := windows.CloseHandle(s.event); err != nil && s.stopErr == nil {
			s.stopErr = os.NewSyscallError("CloseHandle(event)", err)
		}
		if err := windows.CloseHandle(s.stopEvent); err != nil && s.stopErr == nil {
			s.stopErr = os.NewSyscallError("CloseHandle(stop)", err)
		}
	})
	return s.stopErr
}

func (w *windowsWorkspaceWatcher) read(sub *windowsWorkspaceSubscription) {
	defer w.wg.Done()
	defer close(sub.done)
	ready := false
	defer func() {
		if !ready {
			sub.signalReady(fsnotify.ErrClosed)
		}
	}()
	buf := make([]byte, windowsWorkspaceWatchBuffer)
	mask := uint32(windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME | windows.FILE_NOTIFY_CHANGE_LAST_WRITE)
	for {
		if w.stopping(sub) {
			return
		}
		ov := windows.Overlapped{HEvent: sub.event}
		err := windows.ReadDirectoryChanges(sub.handle, &buf[0], uint32(len(buf)), sub.recursive, mask, nil, &ov, 0)
		if err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
			if w.stopping(sub) || errors.Is(err, windows.ERROR_OPERATION_ABORTED) || errors.Is(err, windows.ERROR_INVALID_HANDLE) {
				return
			}
			if !ready {
				sub.signalReady(os.NewSyscallError("ReadDirectoryChanges", err))
				ready = true
				return
			}
			if errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) {
				w.sendError(sub, fsnotify.ErrEventOverflow)
				continue
			}
			w.sendError(sub, os.NewSyscallError("ReadDirectoryChanges", err))
			return
		}
		if !ready {
			sub.signalReady(nil)
			ready = true
		}
		which, err := windows.WaitForMultipleObjects([]windows.Handle{sub.event, sub.stopEvent}, false, windows.INFINITE)
		if err != nil {
			if w.stopping(sub) {
				return
			}
			w.sendError(sub, os.NewSyscallError("WaitForMultipleObjects", err))
			return
		}
		if which == windows.WAIT_OBJECT_0+1 {
			cancelErr := windows.CancelIoEx(sub.handle, &ov)
			if cancelErr != nil && !errors.Is(cancelErr, windows.ERROR_NOT_FOUND) {
				w.sendError(sub, os.NewSyscallError("CancelIoEx", cancelErr))
			}
			var ignored uint32
			if err := windows.GetOverlappedResult(sub.handle, &ov, &ignored, true); err != nil && !errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
				w.sendError(sub, os.NewSyscallError("GetOverlappedResult(cancel)", err))
			}
			return
		}
		if which != windows.WAIT_OBJECT_0 {
			w.sendError(sub, fmt.Errorf("WaitForMultipleObjects: unexpected result %d", which))
			return
		}
		var n uint32
		if err := windows.GetOverlappedResult(sub.handle, &ov, &n, false); err != nil {
			if w.stopping(sub) || errors.Is(err, windows.ERROR_OPERATION_ABORTED) || errors.Is(err, windows.ERROR_INVALID_HANDLE) {
				return
			}
			if errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) || errors.Is(err, windows.ERROR_MORE_DATA) {
				w.sendError(sub, fsnotify.ErrEventOverflow)
				continue
			}
			w.sendError(sub, os.NewSyscallError("GetOverlappedResult", err))
			return
		}
		if n == 0 {
			w.sendError(sub, fsnotify.ErrEventOverflow)
			continue
		}
		if err := w.publishBuffer(sub, buf[:n]); err != nil {
			w.sendError(sub, err)
		}
	}
}

func (s *windowsWorkspaceSubscription) signalReady(err error) {
	s.readyOnce.Do(func() { s.ready <- err })
}

func (w *windowsWorkspaceWatcher) publishBuffer(sub *windowsWorkspaceSubscription, buf []byte) error {
	const header = 12
	for offset := 0; ; {
		if len(buf)-offset < header {
			return fmt.Errorf("ReadDirectoryChanges: malformed notification header")
		}
		next := int(binary.LittleEndian.Uint32(buf[offset:]))
		action := binary.LittleEndian.Uint32(buf[offset+4:])
		nameBytes := int(binary.LittleEndian.Uint32(buf[offset+8:]))
		if nameBytes < 0 || nameBytes%2 != 0 || nameBytes > len(buf)-offset-header {
			return fmt.Errorf("ReadDirectoryChanges: malformed notification name")
		}
		units := make([]uint16, nameBytes/2)
		for i := range units {
			start := offset + header + i*2
			units[i] = binary.LittleEndian.Uint16(buf[start:])
		}
		name := windows.UTF16ToString(units)
		if op := windowsWorkspaceOp(action); op != 0 && name != "" {
			if !w.sendEvent(sub, fsnotify.Event{Name: filepath.Join(sub.path, name), Op: op}) {
				return nil
			}
		}
		if next == 0 {
			return nil
		}
		if next < header || next > len(buf)-offset {
			return fmt.Errorf("ReadDirectoryChanges: invalid notification offset")
		}
		offset += next
	}
}

func windowsWorkspaceOp(action uint32) fsnotify.Op {
	switch action {
	case windows.FILE_ACTION_ADDED, windows.FILE_ACTION_RENAMED_NEW_NAME:
		return fsnotify.Create
	case windows.FILE_ACTION_REMOVED:
		return fsnotify.Remove
	case windows.FILE_ACTION_MODIFIED:
		return fsnotify.Write
	case windows.FILE_ACTION_RENAMED_OLD_NAME:
		return fsnotify.Rename
	default:
		return 0
	}
}

func (w *windowsWorkspaceWatcher) sendEvent(sub *windowsWorkspaceSubscription, ev fsnotify.Event) bool {
	select {
	case <-w.closed:
		return false
	case <-sub.stop:
		return false
	case w.events <- ev:
		return true
	}
}

func (w *windowsWorkspaceWatcher) sendError(sub *windowsWorkspaceSubscription, err error) bool {
	select {
	case <-w.closed:
		return false
	case <-sub.stop:
		return false
	case w.errors <- err:
		return true
	}
}

func (w *windowsWorkspaceWatcher) stopping(sub *windowsWorkspaceSubscription) bool {
	select {
	case <-w.closed:
		return true
	case <-sub.stop:
		return true
	default:
		return false
	}
}

func windowsWorkspaceWatchKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}
