package forward

import (
	"errors"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Event reports a forward's transition, delivered to the Set's onEvent hook.
type Event struct {
	Spec      Spec
	Up        bool
	BoundAddr string // actual bound address (resolves ":0")
	Err       error
}

// Entry is a snapshot of one registered forward.
type Entry struct {
	Spec      Spec
	Up        bool
	BoundAddr string
	LastErr   error
}

// Set is the port-forward registry for one Client. It is safe for concurrent
// use. Local listeners persist across Detach/Attach so a forwarded port stays
// reserved through reconnects; remote listeners are torn down on Detach and
// recreated on Attach.
type Set struct {
	onEvent func(Event)

	mu   sync.Mutex
	ssh  *ssh.Client
	runs map[string]*runner
}

// NewSet creates an empty Set. onEvent may be nil.
func NewSet(onEvent func(Event)) *Set {
	return &Set{onEvent: onEvent, runs: map[string]*runner{}}
}

type runner struct {
	spec     Spec
	local    net.Listener // Local: persistent local listener
	remote   net.Listener // Remote: per-connection remote listener
	up       bool
	lastErr  error
	stop     chan struct{}
	acceptWG sync.WaitGroup
}

// Add registers spec and starts it if the Set is attached. Returns the bound
// address (useful for ":0" local forwards).
func (s *Set) Add(spec Spec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	name := spec.DefaultName()
	spec.Name = name
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[name]; ok {
		return "", ErrDuplicateForward
	}
	r := &runner{spec: spec, stop: make(chan struct{})}
	s.runs[name] = r
	if s.ssh == nil {
		return "", nil // starts on Attach
	}
	bound, err := s.startLocked(r, s.ssh)
	if err != nil {
		delete(s.runs, name)
		return "", err
	}
	return bound, nil
}

// Replace atomically swaps the named registration after the replacement has
// started successfully. If startup fails, the existing forward remains live.
// This is primarily used when a remote serve moves to a new workspace/port.
func (s *Set) Replace(spec Spec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	name := spec.DefaultName()
	spec.Name = name
	s.mu.Lock()
	old := s.runs[name]
	if s.ssh == nil {
		s.mu.Unlock()
		return "", ErrNotAttached
	}
	replacement := &runner{spec: spec, stop: make(chan struct{})}
	bound := ""
	bound, err := s.startLocked(replacement, s.ssh)
	if err != nil {
		s.mu.Unlock()
		return "", err
	}
	s.runs[name] = replacement
	if old != nil {
		// The replacement's Up event is authoritative; suppress a later Down event
		// from retiring the old runner with the same name.
		old.up = false
	}
	s.mu.Unlock()
	if old != nil {
		s.stopRunner(old, true)
	}
	return bound, nil
}

// Remove stops and deregisters the named forward.
func (s *Set) Remove(name string) error {
	s.mu.Lock()
	r, ok := s.runs[name]
	if ok {
		delete(s.runs, name)
	}
	s.mu.Unlock()
	if !ok {
		return errors.New("forward: no such forward: " + name)
	}
	s.stopRunner(r, true)
	return nil
}

// List snapshots all registered forwards.
func (s *Set) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.runs))
	for _, r := range s.runs {
		bound := ""
		if r.local != nil {
			bound = r.local.Addr().String()
		} else if r.remote != nil {
			bound = r.remote.Addr().String()
		}
		out = append(out, Entry{Spec: r.spec, Up: r.up, BoundAddr: bound, LastErr: r.lastErr})
	}
	return out
}

// Attach binds the Set to a (re)connected ssh client and (re)starts every
// forward. Per-forward failures are joined and returned; successfully started
// forwards stay up.
func (s *Set) Attach(cl *ssh.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ssh = cl
	var errs []error
	for _, r := range s.runs {
		if _, err := s.startLocked(r, cl); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Detach drops the current connection. Local listeners stay open (and refuse
// data until re-attached); remote listeners are closed.
func (s *Set) Detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ssh = nil
	for _, r := range s.runs {
		if r.remote != nil {
			_ = r.remote.Close()
			r.remote = nil
		}
		if r.up {
			r.up = false
			s.emit(Event{Spec: r.spec, Up: false})
		}
	}
}

// Close stops all forwards and releases every listener.
func (s *Set) Close() {
	s.mu.Lock()
	runs := s.runs
	s.runs = map[string]*runner{}
	s.ssh = nil
	s.mu.Unlock()
	for _, r := range runs {
		s.stopRunner(r, true)
	}
}

// startLocked starts (or restarts) r on cl. Caller holds s.mu.
func (s *Set) startLocked(r *runner, cl *ssh.Client) (string, error) {
	if r.spec.Direction == Local {
		return s.startLocalLocked(r, cl)
	}
	return s.startRemoteLocked(r, cl)
}

func (s *Set) startLocalLocked(r *runner, cl *ssh.Client) (string, error) {
	if r.local == nil {
		ln, err := net.Listen("tcp", r.spec.BindAddr)
		if err != nil {
			r.lastErr = wrapBind(err)
			s.emit(Event{Spec: r.spec, Up: false, Err: r.lastErr})
			return "", r.lastErr
		}
		r.local = ln
		r.acceptWG.Add(1)
		go s.acceptLocal(r)
	}
	r.up = true
	r.lastErr = nil
	bound := r.local.Addr().String()
	s.emit(Event{Spec: r.spec, Up: true, BoundAddr: bound})
	return bound, nil
}

// acceptLocal accepts on the persistent local listener. Each accepted conn is
// forwarded through whatever ssh client is current at dial time; when detached
// (ssh == nil) the conn is refused.
func (s *Set) acceptLocal(r *runner) {
	defer r.acceptWG.Done()
	for {
		conn, err := r.local.Accept()
		if err != nil {
			select {
			case <-r.stop:
				return
			default:
				return // listener closed
			}
		}
		go s.handleLocalConn(r, conn)
	}
}

func (s *Set) handleLocalConn(r *runner, local net.Conn) {
	s.mu.Lock()
	cl := s.ssh
	s.mu.Unlock()
	if cl == nil {
		_ = local.Close()
		return
	}
	remote, err := cl.Dial("tcp", r.spec.TargetAddr)
	if err != nil {
		_ = local.Close()
		return
	}
	pipe(local, remote)
}

func (s *Set) startRemoteLocked(r *runner, cl *ssh.Client) (string, error) {
	ln, err := cl.Listen("tcp", r.spec.BindAddr)
	if err != nil {
		r.lastErr = wrapBind(err)
		s.emit(Event{Spec: r.spec, Up: false, Err: r.lastErr})
		return "", r.lastErr
	}
	r.remote = ln
	r.up = true
	r.lastErr = nil
	go s.acceptRemote(r, ln)
	bound := ln.Addr().String()
	s.emit(Event{Spec: r.spec, Up: true, BoundAddr: bound})
	return bound, nil
}

func (s *Set) acceptRemote(r *runner, ln net.Listener) {
	for {
		remote, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			local, derr := net.Dial("tcp", r.spec.TargetAddr)
			if derr != nil {
				_ = remote.Close()
				return
			}
			pipe(remote, local)
		}()
	}
}

func (s *Set) stopRunner(r *runner, closeLocal bool) {
	close(r.stop)
	if r.remote != nil {
		_ = r.remote.Close()
		r.remote = nil
	}
	if closeLocal && r.local != nil {
		_ = r.local.Close()
	}
	r.acceptWG.Wait()
	if closeLocal {
		r.local = nil
	}
	if r.up {
		r.up = false
		s.emit(Event{Spec: r.spec, Up: false})
	}
}

func (s *Set) emit(e Event) {
	if s.onEvent != nil {
		s.onEvent(e)
	}
}

// pipe copies bidirectionally between a and b, closing both when either side
// ends. Half-close is best-effort via CloseWrite when supported.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
}

func wrapBind(err error) error {
	if isAddrInUse(err) {
		return errors.Join(ErrBindBusy, err)
	}
	return err
}
