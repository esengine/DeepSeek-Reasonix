package plugin

import (
	"strings"
	"sync"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// tailBuffer keeps the last limit bytes written, for truncating a process's
// captured output to what teardown diagnostics actually need.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
	cut   bool // bytes before buf were dropped to hold the limit
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if b.limit > 0 && len(b.buf) > b.limit {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.limit:]...)
		b.cut = true
	}
	return len(p), nil
}

// String reads the tail in the server's own encoding: cmd.exe on a Chinese
// Windows reports a missing command in the console code page, not UTF-8.
func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(fileencoding.DecodeOutput(b.buf, fileencoding.Cut{Head: b.cut})))
}
