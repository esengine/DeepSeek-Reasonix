package main

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func platformOSVersion() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("Windows %d.%d build %d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}

func platformCPU() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	name, _, err := k.GetStringValue("ProcessorNameString")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

func platformRAMBytes() uint64 {
	var kb uint64
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetPhysicallyInstalledSystemMemory")
	if ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&kb))); ret == 0 {
		return 0
	}
	return kb * 1024
}
