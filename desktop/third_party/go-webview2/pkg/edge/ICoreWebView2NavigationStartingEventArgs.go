//go:build windows

package edge

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type _ICoreWebView2NavigationStartingEventArgsVtbl struct {
	_IUnknownVtbl
	GetURI             ComProc
	GetIsUserInitiated ComProc
	GetIsRedirected    ComProc
	GetRequestHeaders  ComProc
	GetCancel          ComProc
	PutCancel          ComProc
	GetNavigationID    ComProc
}

type ICoreWebView2NavigationStartingEventArgs struct {
	vtbl *_ICoreWebView2NavigationStartingEventArgsVtbl
}

func (i *ICoreWebView2NavigationStartingEventArgs) GetIsUserInitiated() (bool, error) {
	var value int32
	hr, _, _ := i.vtbl.GetIsUserInitiated.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&value)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return false, syscall.Errno(hr)
	}
	return value != 0, nil
}

func (i *ICoreWebView2NavigationStartingEventArgs) GetNavigationID() (uint64, error) {
	var value uint64
	hr, _, _ := i.vtbl.GetNavigationID.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&value)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return 0, syscall.Errno(hr)
	}
	return value, nil
}
