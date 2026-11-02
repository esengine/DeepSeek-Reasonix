package secrets

import (
	"bytes"
	"io"
	"sync"
)

// Stream masks text written in pieces. A trailing fragment that could begin a
// held value is kept back until the rest arrives or Flush, so a value split
// across two writes is still recognised; text that cannot begin one passes on
// at once, so a reader waiting for a line is not delayed.
type Stream struct {
	mu   sync.Mutex
	out  io.Writer
	held []byte
}

// NewStream masks what is written to it before it reaches out.
func NewStream(out io.Writer) *Stream { return &Stream{out: out} }

func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := Masker()
	buf := append(s.held, p...)
	cut := m.cut(buf)
	if cut > 0 {
		if _, err := io.WriteString(s.out, m.String(string(buf[:cut]))); err != nil {
			return 0, err
		}
	}
	s.held = append([]byte(nil), buf[cut:]...)
	return len(p), nil
}

// Flush passes on what was kept back, masked as far as it goes.
func (s *Stream) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.held) == 0 {
		return nil
	}
	held := s.held
	s.held = nil
	_, err := io.WriteString(s.out, Masker().String(string(held)))
	return err
}

// cut is how much of buf may pass on now: all of it but the longest suffix that
// is a proper prefix of some spelling, and never splitting a spelling that buf
// holds whole.
func (m *Mask) cut(buf []byte) int {
	if m == nil {
		return len(buf)
	}
	cut := len(buf)
	for _, v := range m.values {
		for _, form := range v.forms {
			f := []byte(form)
			for i := max(0, len(buf)-len(f)+1); i < len(buf); i++ {
				if i < cut && bytes.HasPrefix(f, buf[i:]) {
					cut = i
					break
				}
			}
		}
	}
	for moved := true; moved; {
		moved = false
		for _, v := range m.values {
			for _, form := range v.forms {
				for at := 0; ; {
					i := bytes.Index(buf[at:], []byte(form))
					if i < 0 {
						break
					}
					start, end := at+i, at+i+len(form)
					if start < cut && end > cut {
						cut, moved = start, true
					}
					at = start + 1
				}
			}
		}
	}
	return cut
}
