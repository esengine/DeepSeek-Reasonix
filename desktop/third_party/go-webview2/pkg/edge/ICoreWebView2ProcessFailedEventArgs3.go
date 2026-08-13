//go:build windows

package edge

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type _ICoreWebView2ProcessFailedEventArgs3Vtbl struct {
	_ICoreWebView2ProcessFailedEventArgs2Vtbl
	GetFailureSourceModulePath ComProc
}

type ICoreWebView2ProcessFailedEventArgs3 struct {
	vtbl *_ICoreWebView2ProcessFailedEventArgs3Vtbl
}

func (i *ICoreWebView2ProcessFailedEventArgs3) Release() uintptr {
	result, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))
	return result
}

func (i *ICoreWebView2ProcessFailedEventArgs3) GetFailureSourceModulePath() (string, error) {
	var value *uint16
	hr, _, _ := i.vtbl.GetFailureSourceModulePath.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&value)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return "", syscall.Errno(hr)
	}
	if value == nil {
		return "", nil
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value), nil
}
