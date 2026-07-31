package loader

import "io"

// Proc represents a procedure in memory.
type Proc interface {
	// Call calls the procedure. r1 and r2 contain the return value. lastErr
	// contains the Windows error value after calling.
	Call(a ...uint64) (r1, r2 uint64, lastErr error)

	// Returns the raw address of this function.
	Addr() uint64
}

// Memory is an interface for a block of allocated virtual memory.
type Memory interface {
	io.ReadWriteSeeker
	io.ReaderAt
	io.WriterAt

	// Free frees the virtual memory region.
	Free()

	// Addr returns the address of the region of memory in the virtual address
	// space.
	Addr() uint64

	// Clear zeros the entire region of memory.
	Clear()

	// Protect changes the memory protection for a subregion of this allocated
	// block. It should match the semantics of the VirtualProtect function on
	// Windows.
	Protect(addr, size uint64, protect int) error
}

// Machine is an abstract machine interface.
type Machine interface {
	// IsArchitectureSupported returns whether or not an architecture is
	// supported by this abstract machine. Machine is a PE machine ID.
	IsArchitectureSupported(machine int) bool

	// GetPageSize returns the size of a memory page on this abstract machine.
	GetPageSize() uint64

	// Alloc performs virtual memory allocation. It should match the semantics
	// of VirtualAlloc/VirtualFree on Windows.
	Alloc(addr, size uint64, allocType, protect int) Memory

	// MemProc returns an object for interfacing with a procedure at addr in
	// the abstract machine's virtual memory space.
	MemProc(addr uint64) Proc
}
