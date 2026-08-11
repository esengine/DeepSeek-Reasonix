//go:build windows

package edge

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type _ICoreWebView2ProcessFailedEventArgsVtbl struct {
	_IUnknownVtbl
	GetProcessFailedKind ComProc
}

type ICoreWebView2ProcessFailedEventArgs struct {
	vtbl *_ICoreWebView2ProcessFailedEventArgsVtbl
}

func (i *ICoreWebView2ProcessFailedEventArgs) queryInterface(iid *GUID, result unsafe.Pointer) error {
	hr, _, _ := i.vtbl.QueryInterface.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(result),
	)
	if windows.Handle(hr) != windows.S_OK {
		return syscall.Errno(hr)
	}
	return nil
}

func (i *ICoreWebView2ProcessFailedEventArgs) GetICoreWebView2ProcessFailedEventArgs2() (*ICoreWebView2ProcessFailedEventArgs2, error) {
	var result *ICoreWebView2ProcessFailedEventArgs2
	err := i.queryInterface(
		NewGUID("{4DAB9422-46FA-4C3E-A5D2-41D2071D3680}"),
		unsafe.Pointer(&result),
	)
	return result, err
}

func (i *ICoreWebView2ProcessFailedEventArgs) GetICoreWebView2ProcessFailedEventArgs3() (*ICoreWebView2ProcessFailedEventArgs3, error) {
	var result *ICoreWebView2ProcessFailedEventArgs3
	err := i.queryInterface(
		NewGUID("{AB667428-094D-5FD1-B480-8B4C0FDBDF2F}"),
		unsafe.Pointer(&result),
	)
	return result, err
}

func (i *ICoreWebView2ProcessFailedEventArgs) GetProcessFailedKind() (COREWEBVIEW2_PROCESS_FAILED_KIND, error) {
	kind := COREWEBVIEW2_PROCESS_FAILED_KIND(0xffffffff)
	hr, _, _ := i.vtbl.GetProcessFailedKind.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&kind)),
	)

	if windows.Handle(hr) != windows.S_OK {
		return 0, syscall.Errno(hr)
	}

	if kind == 0xffffffff {
		return 0, fmt.Errorf("unknown error")
	}

	return kind, nil
}
