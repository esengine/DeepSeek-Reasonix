//go:build linux

package checkenv

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"reasonix/internal/base/proc"
)

var (
	errNamespaceUnreported = errors.New("verification launcher never reported its namespace")
	errNamespaceUnbound    = errors.New("verification namespace could not be bound to its launcher")
	errNamespaceSignal     = errors.New("verification namespace cancellation signal failed")
)

// namespaceReleaseGrace bounds how long a scope whose namespace was never bound
// waits for the last reader of its block pipe to exit.
var namespaceReleaseGrace = 5 * time.Second

type scopeSettlement int

const (
	scopePending    scopeSettlement = iota
	scopeExited                     // PID1's pidfd reported its exit
	scopeUnreleased                 // no process can read the block pipe, so the payload can never run
)

type namespaceScope struct {
	mu        sync.Mutex
	info      *os.File
	infoWrite *os.File
	block     *os.File
	unblock   *os.File
	pidfd     int
	cancelled bool
	started   bool
	settled   scopeSettlement
	done      chan struct{}
	monitor   *os.Process
	err       error
	bind      func(int, uint64, *os.Process) (int, error)
	ctx       context.Context
	signal    func(int) error
}

func newNamespaceScope() *namespaceScope {
	return &namespaceScope{pidfd: -1, done: make(chan struct{}), bind: bindNamespacePID,
		signal: func(fd int) error { return unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0) }}
}

func (s *namespaceScope) prepare() error {
	probe, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return errors.Join(ErrUnsupported, err)
	}
	unix.Close(probe)
	s.info, s.infoWrite, err = os.Pipe()
	if err == nil {
		s.block, s.unblock, err = os.Pipe()
	}
	if err != nil {
		s.release()
	}
	return err
}

func (i *Invocation) Started(cmd *exec.Cmd) {
	s := i.scope
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.monitor = cmd.Process
	s.infoWrite.Close()
	s.block.Close()
	s.mu.Unlock()
	go s.observe()
}

func (i *Invocation) Cancel() error {
	s := i.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
	if s.pidfd >= 0 {
		return s.cancelLocked()
	}
	return nil
}

func (i *Invocation) WaitScope() error { return i.scope.wait() }

func (s *namespaceScope) wait() error {
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if started {
		<-s.done
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if started && s.settled == scopePending {
		return errors.Join(s.err, proc.ErrScopeCompletionUnknown)
	}
	return s.err
}

func (s *namespaceScope) completed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settled != scopePending
}

// stranded reports a scope whose observation ended without proving that its
// payload can no longer run; its owner releases only what cannot unblock it.
func (s *namespaceScope) stranded() bool {
	select {
	case <-s.done:
	default:
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settled == scopePending
}

// PID1 cannot execute its payload until its pidfd and namespace identity agree
// with the monitor's setup report. Cancellation preserves the waitpid owner.
func (s *namespaceScope) observe() {
	defer close(s.done)
	var info struct {
		PID       int    `json:"child-pid"`
		Namespace uint64 `json:"pid-namespace"`
	}
	err := json.NewDecoder(io.LimitReader(s.info, 1<<20)).Decode(&info)
	cause := errNamespaceUnreported
	s.mu.Lock()
	if err == nil && info.PID > 0 {
		cause = errNamespaceUnbound
		s.pidfd, err = s.bind(info.PID, info.Namespace, s.monitor)
	}
	fd := s.pidfd
	s.cancelled = s.cancelled || s.ctx.Err() != nil
	if err != nil || fd < 0 {
		s.err = errors.Join(ErrUnsupported, cause, err)
		if fd < 0 {
			_ = s.monitor.Kill()
			unblock := s.unblock
			s.mu.Unlock()
			unreadable := noBlockReader(unblock, namespaceReleaseGrace)
			s.mu.Lock()
			if unreadable {
				s.settled = scopeUnreleased
			}
			s.mu.Unlock()
			return
		}
		if err := s.cancelLocked(); err != nil {
			s.mu.Unlock()
			return
		}
	} else if !s.cancelled || s.cancelLocked() == nil {
		s.unblock.Close()
		s.unblock = nil
	}
	s.mu.Unlock()
	for {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, err := unix.Poll(poll, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		s.mu.Lock()
		if err == nil && poll[0].Revents&unix.POLLIN != 0 {
			s.settled = scopeExited
		} else {
			s.err = errors.Join(s.err, ErrUnsupported, err)
		}
		unix.Close(fd)
		s.pidfd = -1
		s.mu.Unlock()
		return
	}
}

func (s *namespaceScope) cancelLocked() error {
	err := s.signal(s.pidfd)
	if err != nil && !errors.Is(err, unix.ESRCH) {
		s.err = errors.Join(s.err, errNamespaceSignal, err)
		_ = s.monitor.Kill()
		return err
	}
	return nil
}

func bindNamespacePID(pid int, namespace uint64, monitor *os.Process) (int, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return -1, err
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	separator := strings.LastIndex(string(stat), ") ")
	var fields []string
	if separator >= 0 {
		fields = strings.Fields(string(stat[separator+2:]))
	}
	if err != nil || monitor == nil || len(fields) < 2 || fields[1] != strconv.Itoa(monitor.Pid) {
		unix.Close(fd)
		return -1, errors.Join(ErrUnsupported, err)
	}
	if err := monitor.Signal(unix.Signal(0)); err != nil {
		unix.Close(fd)
		return -1, errors.Join(ErrUnsupported, err)
	}
	link, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/ns/pid")
	if err != nil || namespace == 0 || link != "pid:["+strconv.FormatUint(namespace, 10)+"]" {
		return fd, errors.Join(ErrUnsupported, err)
	}
	return fd, nil
}

// noBlockReader waits until every holder of the block pipe's read end has
// exited. PID1 runs its payload only by reading that pipe, so afterwards
// closing the write end can release nothing.
func noBlockReader(unblock *os.File, timeout time.Duration) bool {
	raw, err := unblock.SyscallConn()
	if err != nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	unreadable := false
	_ = raw.Control(func(fd uintptr) {
		for {
			wait := -1
			if timeout >= 0 {
				wait = int(max(time.Until(deadline), 0) / time.Millisecond)
			}
			poll := []unix.PollFd{{Fd: int32(fd)}}
			count, err := unix.Poll(poll, wait)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			unreadable = err == nil && count == 1 && poll[0].Revents&unix.POLLERR != 0
			return
		}
	})
	return unreadable
}

// release closes every descriptor that cannot start the payload. Closing the
// block pipe's write end can, so an unsettled scope hands it to a waiter that
// closes it once no reader remains.
func (s *namespaceScope) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range []*os.File{s.info, s.infoWrite, s.block} {
		if f != nil {
			_ = f.Close()
		}
	}
	unblock := s.unblock
	s.info, s.infoWrite, s.block, s.unblock = nil, nil, nil, nil
	switch {
	case unblock == nil:
	case !s.started || s.settled != scopePending:
		_ = unblock.Close()
	default:
		go func() {
			for !noBlockReader(unblock, -1) {
				time.Sleep(time.Second)
			}
			_ = unblock.Close()
		}()
	}
}
