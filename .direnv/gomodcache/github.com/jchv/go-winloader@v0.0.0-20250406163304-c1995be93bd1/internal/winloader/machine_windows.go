package winloader

import (
	"github.com/jchv/go-winloader/internal/loader"
	"github.com/jchv/go-winloader/internal/vmem"
)

// NativeMachine is a loader.Machine implementation that uses the native
// machine the binary is running on.
type NativeMachine struct{}

// IsArchitectureSupported implements loader.Machine.
func (NativeMachine) IsArchitectureSupported(machine int) bool {
	return machine == NativeArch
}

func (NativeMachine) GetPageSize() uint64 {
	return vmem.GetPageSize()
}

// Alloc implements loader.Machine.
func (NativeMachine) Alloc(addr, size uint64, allocType, protect int) loader.Memory {
	if mem := vmem.Alloc(addr, size, allocType, protect); mem != nil {
		return mem
	}
	return nil
}

// MemProc implements loader.MemProc.
func (NativeMachine) MemProc(addr uint64) loader.Proc {
	return Proc(addr)
}
