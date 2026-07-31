//go:build windows

package wintoast

import (
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

var (
	// Define procs that go-ole doesn't provide. This is how we register our Go-implemented
	// COM objects.
	modcombase              = windows.NewLazySystemDLL("combase.dll")
	procRegisterClassObject = modcombase.NewProc("CoRegisterClassObject")
)

// registerClassFactory teaches the Windows Runtime about our factory that can allocate
// instances of our ActivationCallback.
func registerClassFactory(factory *IClassFactory) error {
	// cookie is used as a handle to this class. It is used when calling CoRevokeClassObject
	// which unregisters the class. We don't need it until we plan to revoke this registration
	// for some reason.
	var cookie int64
	hr, _, _ := procRegisterClassObject.Call(
		uintptr(unsafe.Pointer(GUID_ImplNotificationActivationCallback)),
		uintptr(unsafe.Pointer(factory)),
		uintptr(ole.CLSCTX_LOCAL_SERVER),
		uintptr(1), /* REGCLS_MULTIPLEUSE */
		uintptr(unsafe.Pointer(&cookie)),
	)
	if hr != ole.S_OK {
		return ole.NewError(hr)
	}
	return nil
}
