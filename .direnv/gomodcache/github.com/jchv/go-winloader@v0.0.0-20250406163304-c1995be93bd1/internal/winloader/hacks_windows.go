package winloader

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdllModule                    = windows.NewLazySystemDLL("ntdll")
	ntdllNtQueryInformationProcess = ntdllModule.NewProc("NtQueryInformationProcess")
)

type _ProcessBasicInformation struct {
	Reserved1       uintptr
	PebBaseAddress  uintptr
	Reserved2       [2]uintptr
	UniqueProcessID uintptr
	Reserved3       uintptr
}

type _PEB struct {
	Reserved1     [2]byte
	BeingDebugged byte
	Reserved2     [1]byte
	Reserved3     [2]uintptr
	Ldr           uintptr
}

type _List struct {
	Front uintptr
	Back  uintptr
}

type _PEBLoaderData struct {
	Length                          uint32
	Initialized                     uint8
	Padding                         [3]uint8
	SsHandle                        uintptr
	InLoadOrderModuleList           _List
	InMemoryOrderModuleList         _List
	InInitializationOrderModuleList _List
}

// MakePEBEntryForModule is a hack that inserts an entry for the loaded module
// into the PEB loader data to make it appear to Windows functions.
func MakePEBEntryForModule() error {
	process := -1
	sizeNeeded := uint32(0)
	pbi := _ProcessBasicInformation{}
	status, _, _ := ntdllNtQueryInformationProcess.Call(
		uintptr(process),
		0,
		uintptr(unsafe.Pointer(&pbi)),
		unsafe.Sizeof(pbi),
		uintptr(unsafe.Pointer(&sizeNeeded)),
	)
	if status != 0 {
		return fmt.Errorf("NtQueryInformationProcess failed: %08x", status)
	}

	// TODO: Implement attempt to insert module entry.
	return errors.New("unimplemented")
}

// GetProcessHInstance gets the HINSTANCE for the current process.
func GetProcessHInstance() (uintptr, error) {
	var hinstance windows.Handle
	windows.GetModuleHandleEx(0, nil, &hinstance)
	return uintptr(hinstance), nil
}
