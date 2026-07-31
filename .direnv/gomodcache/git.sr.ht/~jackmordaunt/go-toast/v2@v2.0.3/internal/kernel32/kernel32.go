//go:build windows

package kernel32

import (
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type (
	heapHandle = uintptr
	win32Error uint32
	heapFlags  uint32
)

const (
	heapNone       heapFlags = 0
	heapZeroMemory heapFlags = 8 // The allocated memory will be initialized to zero.
)

var (
	libKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pHeapFree       uintptr
	pHeapAlloc      uintptr
	pGetProcessHeap uintptr

	hHeap heapHandle
)

func init() {
	hHeap, _ = getProcessHeap()
}

// Malloc allocates the given amount of bytes in the heap
func Malloc(size uintptr) unsafe.Pointer {
	return heapAlloc(hHeap, heapZeroMemory, size)
}

// Free releases the given unsafe pointer from the heap
func Free(inst unsafe.Pointer) {
	_, _ = heapFree(hHeap, heapNone, inst)
}

// https://docs.microsoft.com/en-us/windows/win32/api/heapapi/nf-heapapi-heapalloc
func heapAlloc(hHeap heapHandle, dwFlags heapFlags, dwBytes uintptr) unsafe.Pointer {
	addr := getProcAddr(&pHeapAlloc, libKernel32, "HeapAlloc")
	allocatedPtr, _, _ := syscall.SyscallN(addr, hHeap, uintptr(dwFlags), dwBytes)
	// Since this pointer is allocated in the heap by Windows, it will never be
	// GCd by Go, so this is a safe operation.
	// But linter thinks it is not (probably because we are not using CGO) and fails.
	return unsafe.Pointer(allocatedPtr) //nolint:gosec,govet
}

// https://docs.microsoft.com/en-us/windows/win32/api/heapapi/nf-heapapi-heapfree
func heapFree(hHeap heapHandle, dwFlags heapFlags, lpMem unsafe.Pointer) (bool, win32Error) {
	addr := getProcAddr(&pHeapFree, libKernel32, "HeapFree")
	ret, _, err := syscall.SyscallN(addr, hHeap, uintptr(dwFlags), uintptr(lpMem))
	return ret == 0, win32Error(err)
}

func getProcessHeap() (heapHandle, win32Error) {
	addr := getProcAddr(&pGetProcessHeap, libKernel32, "GetProcessHeap")
	ret, _, err := syscall.SyscallN(addr)
	return ret, win32Error(err)
}

func getProcAddr(pAddr *uintptr, lib *windows.LazyDLL, procName string) uintptr {
	addr := atomic.LoadUintptr(pAddr)
	if addr == 0 {
		addr = lib.NewProc(procName).Addr()
		atomic.StoreUintptr(pAddr, addr)
	}
	return addr
}
