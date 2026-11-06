package terminal

import "sync"

// ring keeps the newest bytes a terminal wrote, up to a fixed size, and counts
// every byte ever written so a reader can say where it stopped.
type ring struct {
	mu    sync.Mutex
	buf   []byte
	size  int
	total uint64
}

func newRing(size int) *ring { return &ring{size: size} }

// write appends p and returns the offset just past it.
func (r *ring) write(p []byte) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total += uint64(len(p))
	if len(p) >= r.size {
		r.buf = append(r.buf[:0], p[len(p)-r.size:]...)
		return r.total
	}
	if over := len(r.buf) + len(p) - r.size; over > 0 {
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
	r.buf = append(r.buf, p...)
	return r.total
}

// since returns what was written after offset, and the offset it reaches. A
// reader that fell behind the ring's start gets what is still held.
func (r *ring) since(offset uint64) ([]byte, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.total - uint64(len(r.buf))
	if offset < start {
		offset = start
	}
	if offset > r.total {
		offset = r.total
	}
	return append([]byte(nil), r.buf[offset-start:]...), r.total
}
