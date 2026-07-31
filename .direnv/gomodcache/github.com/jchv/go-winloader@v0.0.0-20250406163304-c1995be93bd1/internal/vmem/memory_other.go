//go:build !windows
// +build !windows

package vmem

// Memory represents a raw block of memory.
type Memory struct {
	data []byte
}

// GetPageSize returns the size of a memory page.
func GetPageSize() uint64 {
	panic("not implemented")
}

// Alloc allocates memory at addr of size with allocType and protect.
// It returns nil if it fails.
func Alloc(addr, size uint64, allocType, protect int) *Memory {
	panic("not implemented")
}

// Get returns a range of existing memory. If the range is not a block of
// allocated memory, the returned memory will pagefault when accessed.
func Get(addr, size uint64) *Memory {
	panic("not implemented")
}

// Free frees the block of memory.
func (m *Memory) Free() {
	panic("not implemented")
}

// Addr returns the actual address of the memory.
func (m *Memory) Addr() uint64 {
	panic("not implemented")
}

// Read implements the io.Reader interface.
func (m *Memory) Read(b []byte) (n int, err error) {
	panic("not implemented")
}

// ReadAt implements the io.ReaderAt interface.
func (m *Memory) ReadAt(b []byte, off int64) (n int, err error) {
	panic("not implemented")
}

// Write implements the io.Writer interface.
func (m *Memory) Write(b []byte) (n int, err error) {
	panic("not implemented")
}

// WriteAt implements the io.WriterAt interface.
func (m *Memory) WriteAt(b []byte, off int64) (n int, err error) {
	panic("not implemented")
}

// Seek implements the io.Seeker interface.
func (m *Memory) Seek(offset int64, whence int) (int64, error) {
	panic("not implemented")
}

// Protect changes the memory protection for a range of memory.
func (m *Memory) Protect(addr, size uint64, protect int) error {
	panic("not implemented")
}

// Clear sets all bytes in the memory block to zero.
func (m *Memory) Clear() {
	panic("not implemented")
}
