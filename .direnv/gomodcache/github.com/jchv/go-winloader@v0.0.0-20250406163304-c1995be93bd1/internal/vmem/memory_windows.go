//go:build windows
// +build windows

package vmem

import (
	"errors"
	"io"
	"reflect"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                      = windows.NewLazySystemDLL("kernel32")
	kernel32VirtualAlloc          = kernel32.NewProc("VirtualAlloc")
	kernel32VirtualFree           = kernel32.NewProc("VirtualFree")
	kernel32VirtualProtect        = kernel32.NewProc("VirtualProtect")
	kernel32GetNativeSystemInfo   = kernel32.NewProc("GetNativeSystemInfo")
	kernel32FlushInstructionCache = kernel32.NewProc("FlushInstructionCache")
	kernel32GetCurrentProcess     = kernel32.NewProc("GetCurrentProcess")

	pageSize     uint64
	pageSizeOnce sync.Once
)

// getCurrentProcess returns the current process handle.
func getCurrentProcess() uintptr {
	r, _, _ := kernel32GetCurrentProcess.Call()
	return uintptr(r)
}

// GetPageSize returns the size of a memory page.
func GetPageSize() uint64 {
	pageSizeOnce.Do(func() {
		if kernel32GetNativeSystemInfo.Find() != nil {
			pageSize = 0x1000
		}
		info := systemInfo{}
		kernel32GetNativeSystemInfo.Call(uintptr(unsafe.Pointer(&info)))
		pageSize = uint64(info.dwPageSize)
	})
	return pageSize
}

// Memory represents a raw block of memory.
type Memory struct {
	data []byte
	i    int64
}

// Alloc allocates memory at addr of size with allocType and protect.
// It returns nil if it fails.
func Alloc(addr, size uint64, allocType, protect int) *Memory {
	r, _, _ := kernel32VirtualAlloc.Call(uintptr(addr), uintptr(size), uintptr(allocType), uintptr(protect))
	if r == 0 {
		return nil
	}
	return Get(uint64(r), size)
}

// Get returns a range of existing memory. If the range is not a block of
// allocated memory, the returned memory will pagefault when accessed.
func Get(addr, size uint64) *Memory {
	m := &Memory{}
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&m.data))
	sh.Data = uintptr(addr)
	sh.Len = int(size)
	sh.Cap = int(size)
	return m
}

// Free frees the block of memory.
func (m *Memory) Free() {
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&m.data))
	kernel32VirtualFree.Call(sh.Data, 0, memRelease)
	m.data = nil
}

// Addr returns the actual address of the memory.
func (m *Memory) Addr() uint64 {
	return uint64((*reflect.SliceHeader)(unsafe.Pointer(&m.data)).Data)
}

// Read implements the io.Reader interface.
func (m *Memory) Read(b []byte) (n int, err error) {
	if m.i >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n = copy(b, m.data[m.i:])
	m.i += int64(n)
	return n, nil
}

// ReadAt implements the io.ReaderAt interface.
func (m *Memory) ReadAt(b []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n = copy(b, m.data[off:])
	if n < len(b) {
		return n, io.EOF
	}
	return n, nil
}

// Write implements the io.Writer interface.
func (m *Memory) Write(b []byte) (n int, err error) {
	if m.i >= int64(len(m.data)) {
		return 0, io.ErrShortWrite
	}
	n = copy(m.data[m.i:], b)
	if kernel32FlushInstructionCache.Find() == nil {
		kernel32FlushInstructionCache.Call(getCurrentProcess(), uintptr(unsafe.Pointer(&m.data[m.i])), uintptr(n))
	}
	m.i += int64(n)
	return n, nil
}

// WriteAt implements the io.WriterAt interface.
func (m *Memory) WriteAt(b []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= int64(len(m.data)) {
		return 0, io.ErrShortWrite
	}
	n = copy(m.data[off:], b)
	if kernel32FlushInstructionCache.Find() == nil {
		kernel32FlushInstructionCache.Call(getCurrentProcess(), uintptr(unsafe.Pointer(&m.data[off])), uintptr(n))
	}
	if n < len(b) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

// Seek implements the io.Seeker interface.
func (m *Memory) Seek(offset int64, whence int) (int64, error) {
	var n int64
	switch whence {
	case io.SeekStart:
		n = offset
	case io.SeekCurrent:
		n = m.i + offset
	case io.SeekEnd:
		n = int64(len(m.data)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if n < 0 {
		return 0, errors.New("negative position")
	}
	m.i = n
	return n, nil
}

// Clear sets all bytes in the memory block to zero.
func (m *Memory) Clear() {
	for i := range m.data {
		m.data[i] = 0
	}
	if kernel32FlushInstructionCache.Find() == nil {
		kernel32FlushInstructionCache.Call(getCurrentProcess(), uintptr(unsafe.Pointer(&m.data[0])), uintptr(len(m.data)))
	}
}

// Protect changes the memory protection for a range of memory.
func (m *Memory) Protect(addr, size uint64, protect int) error {
	// TODO: error handling
	oldProtect := uint32(0)
	kernel32VirtualProtect.Call(uintptr(m.Addr()+addr), uintptr(size), uintptr(protect), uintptr(unsafe.Pointer(&oldProtect)))
	return nil
}
