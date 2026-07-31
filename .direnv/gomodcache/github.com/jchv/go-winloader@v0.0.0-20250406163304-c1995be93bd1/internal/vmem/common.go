package vmem

const (
	memDecommit = 0x4000
	memRelease  = 0x8000
)

type systemInfo struct {
	wProcessorArchitecture      uint16
	wReserved                   uint16
	dwPageSize                  uint32
	lpMinimumApplicationAddress uintptr
	lpMaximumApplicationAddress uintptr
	dwActiveProcessorMask       uintptr
	dwNumberOfProcessors        uint32
	dwProcessorType             uint32
	dwAllocationGranularity     uint32
	wProcessorLevel             uint16
	wProcessorRevision          uint16
}

// Enumeration of allocation type values.
const (
	MemCommit    = 0x00001000
	MemReserve   = 0x00002000
	MemReset     = 0x00080000
	MemResetUndo = 0x10000000
)

// Enumeration of protection levels.
const (
	PageNoAccess         = 0x01
	PageReadOnly         = 0x02
	PageReadWrite        = 0x04
	PageWriteCopy        = 0x08
	PageExecute          = 0x10
	PageExecuteRead      = 0x20
	PageExecuteReadWrite = 0x40
	PageExecuteWriteCopy = 0x80
)

// RoundDown rounds an address up to a given multiple of size. Size must be a
// power of two.
func RoundDown(addr uint64, size uint64) uint64 {
	if size&(size-1) != 0 {
		panic("alignment size is not a power of two")
	}
	return addr &^ (size - 1)
}

// RoundUp rounds an address up to a given multiple of size. Size must be a
// power of two.
func RoundUp(addr uint64, size uint64) uint64 {
	return RoundDown(addr+size-1, size)
}
