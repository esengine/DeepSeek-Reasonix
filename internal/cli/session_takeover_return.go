package cli

import (
	"fmt"
	"sync"

	"reasonix/internal/agent"
)

// returnCurrentMirror refreshes the binding after pushLocked while sendMu
// fences the generation used by both the reverse reservation and mirror-end.
func (m *cliTakeoverManager) returnCurrentMirror(expectedPath string, retire func(*cliTakeoverBinding) error) error {
	return m.returnMirrorTransaction(expectedPath, false, false, retire)
}

// returnMirrorTransaction is the single owner for retiring an active mirror.
// returnMu prevents Activate/session switches, sendMu fences re-adoption, and
// the binding is re-read after pushLocked so the reverse reservation and
// mirror-end always use one current generation. Snapshot is requested for
// reclaim/exit, after returnMu excludes a different session but before the
// sender lock is held across disk I/O and event fan-out.
func (m *cliTakeoverManager) returnMirrorTransaction(expectedPath string, allowReclaim, snapshot bool, retire func(*cliTakeoverBinding) error) error {
	if m == nil || retire == nil {
		return fmt.Errorf("takeover return transaction unavailable")
	}
	expectedPath = agent.CanonicalSessionPath(expectedPath)
	m.returnMu.Lock()
	defer m.returnMu.Unlock()
	if !allowReclaim && m.reclaiming.Load() {
		return fmt.Errorf("the remote side is reclaiming the current session")
	}
	current, ctrl, _, _ := m.snapshot()
	if current == nil || m.returned.Load() {
		return nil
	}
	if expectedPath != "" && agent.CanonicalSessionPath(current.path) != expectedPath {
		return fmt.Errorf("current takeover mirror changed during session switch")
	}
	if snapshot && ctrl != nil {
		if err := ctrl.Snapshot(); err != nil {
			return err
		}
	}
	m.sendMu.Lock()
	unlockError := func(err error) error { m.sendMu.Unlock(); return err }
	current, _, _, _ = m.snapshot()
	if current == nil || m.returned.Load() || expectedPath != "" && agent.CanonicalSessionPath(current.path) != expectedPath {
		return unlockError(fmt.Errorf("current takeover mirror changed during session switch"))
	}
	if !m.pushLocked(false) {
		return unlockError(fmt.Errorf("current takeover mirror stopped during session switch"))
	}
	current, _, _, _ = m.snapshot()
	if current == nil || m.returned.Load() || expectedPath != "" && agent.CanonicalSessionPath(current.path) != expectedPath {
		return unlockError(fmt.Errorf("current takeover mirror changed during session switch"))
	}
	if !allowReclaim && m.reclaiming.Load() {
		return unlockError(fmt.Errorf("the remote side is reclaiming the current session"))
	}
	if err := retire(current); err != nil {
		return unlockError(err)
	}
	m.returned.Store(true)
	m.mirrorEndLocked(current)
	m.mu.Lock()
	started, stop, done := m.started, m.stop, m.done
	m.binding = nil
	m.queue.Reset()
	m.revision++
	m.failures = 0
	m.mu.Unlock()
	m.sendMu.Unlock()
	if started {
		m.stopOnce.Do(func() { close(stop) })
		<-done
	}
	m.mu.Lock()
	m.started, m.wake, m.stop, m.done = false, nil, nil, nil
	m.stopOnce = sync.Once{}
	m.reclaiming.Store(false)
	if !allowReclaim {
		// A session switch is followed by Activate for a different mirror (or
		// continues without one). Terminal reclaim/Close keeps returned true so
		// the outer TUI/headless lifecycle can observe that ownership was yielded.
		m.returned.Store(false)
	}
	m.mu.Unlock()
	return nil
}
