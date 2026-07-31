package winloader

import (
	"syscall"

	"github.com/jchv/go-winloader/internal/loader"
	"golang.org/x/sys/windows"
)

// Proc is a Proc implementation using a native procedure.
type Proc uintptr

// Call calls a native procedure.
func (p Proc) Call(a ...uint64) (r1, r2 uint64, lastErr error) {
	var ua []uintptr
	for _, e := range a {
		ua = append(ua, uintptr(e))
	}
	n1, n2, lastErr := syscall.SyscallN(uintptr(p), ua...)
	return uint64(n1), uint64(n2), lastErr
}

// Addr gets the native address of the procedure.
func (p Proc) Addr() uint64 {
	return uint64(p)
}

// Library is a module implementation using the native Windows loader.
type Library windows.Handle

// Proc implements loader.Module
func (l Library) Proc(name string) loader.Proc {
	proc, _ := windows.GetProcAddress(windows.Handle(l), name)
	if proc == 0 {
		return nil
	}
	return Proc(proc)
}

// Ordinal implements loader.Module
func (l Library) Ordinal(ordinal uint64) loader.Proc {
	proc, _ := windows.GetProcAddressByOrdinal(windows.Handle(l), uintptr(ordinal))
	if proc == 0 {
		return nil
	}
	return Proc(proc)
}

// Free implements loader.Module
func (l Library) Free() error {
	return windows.FreeLibrary(windows.Handle(l))
}

// Loader is a loader that uses the native Windows library loader.
type Loader struct{}

// Load loads a module into memory.
func (Loader) Load(libname string) (loader.Module, error) {
	handle, err := windows.LoadLibrary(libname)
	if err != nil {
		return nil, err
	}
	return Library(handle), nil
}
