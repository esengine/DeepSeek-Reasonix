package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"sort"
	"sync"
	"time"
)

const (
	ringSize     = 1 << 20
	subBuffer    = 256
	maxTerminals = 16
	defaultCols  = 80
	defaultRows  = 24
	maxDim       = 1000
)

// Options say how one shell is started. Env is used as given when it is set and
// is the process environment otherwise: the caller decides what the person's
// shell may inherit.
type Options struct {
	Shell string
	Args  []string
	Dir   string
	Env   []string
	Cols  uint16
	Rows  uint16
	// Name is what the person sees the shell called.
	Name string
}

// Info is a terminal as a list shows it.
type Info struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Shell    string `json:"shell"`
	Dir      string `json:"dir"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Running  bool   `json:"running"`
	ExitCode *int   `json:"exitCode,omitempty"`
	// Busy is set while the shell waits on a command it started.
	Busy    bool      `json:"busy"`
	Created time.Time `json:"created"`
}

// Chunk is output a subscriber has not seen.
type Chunk []byte

// Subscription is one reader of a terminal's output.
type Subscription struct {
	// Replay is what the terminal wrote before this subscription began, up to
	// what the ring still holds.
	Replay []byte
	// Output carries what is written afterwards. It closes when the terminal
	// ends, or when the reader fell too far behind to keep up.
	Output <-chan Chunk
	cancel func()
}

// Cancel stops the subscription. It is safe to call more than once.
func (s *Subscription) Cancel() { s.cancel() }

// Session is one running shell.
type Session struct {
	id      string
	opts    Options
	created time.Time
	out     *ring

	mu     sync.Mutex
	pty    pty
	cols   uint16
	rows   uint16
	subs   map[int]chan Chunk
	nextID int
	exited bool
	code   int
	done   chan struct{}
}

// Manager keeps the terminals of one host.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager returns an empty Manager.
func NewManager() *Manager { return &Manager{sessions: map[string]*Session{}} }

func clamp(v uint16, fallback uint16) uint16 {
	if v == 0 {
		return fallback
	}
	if v > maxDim {
		return maxDim
	}
	return v
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Open starts a shell and returns its Session.
func (m *Manager) Open(o Options) (*Session, error) {
	if o.Shell == "" {
		return nil, fail(CodeShellMissing, "no shell was given")
	}
	o.Cols, o.Rows = clamp(o.Cols, defaultCols), clamp(o.Rows, defaultRows)
	if o.Dir != "" {
		if st, err := os.Stat(o.Dir); err != nil || !st.IsDir() {
			return nil, fail(CodeStartFailed, "%s is not a folder", o.Dir)
		}
	}
	m.mu.Lock()
	if len(m.sessions) >= maxTerminals {
		m.mu.Unlock()
		return nil, fail(CodeLimit, "at most %d terminals are open", maxTerminals)
	}
	m.mu.Unlock()
	s := &Session{id: newID(), opts: o, created: time.Now(), out: newRing(ringSize), subs: map[int]chan Chunk{}, cols: o.Cols, rows: o.Rows}
	if err := s.start(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
	return s, nil
}

// Get returns a terminal by id.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	return nil, fail(CodeNotFound, "no terminal %s", id)
}

// List is every open terminal, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].created.Before(all[j].created) })
	out := make([]Info, 0, len(all))
	for _, s := range all {
		out = append(out, s.Info())
	}
	return out
}

// Close ends a terminal and forgets it.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return fail(CodeNotFound, "no terminal %s", id)
	}
	s.shutdown()
	return nil
}

// CloseAll ends every terminal; the host calls it as it exits.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := m.sessions
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		s.shutdown()
	}
}

func (s *Session) start() error {
	o := s.opts
	o.Cols, o.Rows = s.cols, s.rows
	p, err := startPty(o)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.pty, s.exited, s.done = p, false, make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go s.pump(p, done)
	return nil
}

// flushGrace is how long the last of a shell's output is given to arrive after
// the shell has ended, before the console is closed to end the read.
const flushGrace = 300 * time.Millisecond

// pump copies the shell's output to the ring and the subscribers until it ends.
// Reading does not end by itself on every platform when the shell does, so the
// shell's end is waited for on its own and closing the console is what stops the read.
func (s *Session) pump(p pty, done chan struct{}) {
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				chunk := append(Chunk(nil), buf[:n]...)
				s.mu.Lock()
				s.out.write(chunk)
				for id, ch := range s.subs {
					select {
					case ch <- chunk:
					default:
						close(ch)
						delete(s.subs, id)
					}
				}
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	code, _ := p.Wait()
	select {
	case <-readDone:
	case <-time.After(flushGrace):
	}
	_ = p.Close()
	<-readDone
	s.mu.Lock()
	s.exited, s.code = true, code
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
	close(done)
}

// ID is the terminal's identity.
func (s *Session) ID() string { return s.id }

// Info is the terminal as a list shows it.
func (s *Session) Info() Info {
	s.mu.Lock()
	pt, exited := s.pty, s.exited
	s.mu.Unlock()
	busy := !exited && pt != nil && pt.Busy()
	s.mu.Lock()
	defer s.mu.Unlock()
	info := Info{Busy: busy, ID: s.id, Name: s.opts.Name, Shell: s.opts.Shell, Dir: s.opts.Dir, Cols: int(s.cols), Rows: int(s.rows), Running: !s.exited, Created: s.created}
	if s.exited {
		c := s.code
		info.ExitCode = &c
	}
	return info
}

// Write sends the person's keystrokes to the shell.
func (s *Session) Write(p []byte) error {
	s.mu.Lock()
	pt, exited := s.pty, s.exited
	s.mu.Unlock()
	if exited {
		return fail(CodeExited, "the shell has ended")
	}
	_, err := pt.Write(p)
	return err
}

// Resize tells the shell its window changed size.
func (s *Session) Resize(cols, rows uint16) error {
	cols, rows = clamp(cols, defaultCols), clamp(rows, defaultRows)
	s.mu.Lock()
	pt, exited := s.pty, s.exited
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	if exited {
		return nil
	}
	return pt.Resize(cols, rows)
}

// Subscribe starts reading the terminal's output: what it held, then what comes.
func (s *Session) Subscribe() *Subscription {
	ch := make(chan Chunk, subBuffer)
	s.mu.Lock()
	replay, _ := s.out.since(0)
	id := s.nextID
	s.nextID++
	if s.exited {
		close(ch)
	} else {
		s.subs[id] = ch
	}
	s.mu.Unlock()
	return &Subscription{Replay: replay, Output: ch, cancel: func() {
		s.mu.Lock()
		if c, ok := s.subs[id]; ok {
			close(c)
			delete(s.subs, id)
		}
		s.mu.Unlock()
	}}
}

// Restart starts the shell again in this terminal once it has ended.
func (s *Session) Restart() error {
	s.mu.Lock()
	running := !s.exited
	s.mu.Unlock()
	if running {
		return nil
	}
	return s.start()
}

// Done closes when the current shell has ended.
func (s *Session) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func (s *Session) shutdown() {
	s.mu.Lock()
	pt, exited, done := s.pty, s.exited, s.done
	s.mu.Unlock()
	if exited || pt == nil {
		return
	}
	_ = pt.Kill()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
