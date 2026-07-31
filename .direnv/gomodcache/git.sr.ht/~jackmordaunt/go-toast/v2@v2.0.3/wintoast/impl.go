//go:build windows

// This file contains our pure-Go implementations of two COM objects that we need
// to render toast notifications: IClassFactory and INotificationActivationCallback.
//
// More specifically we allocate the C callable functions that can be used to populate
// the vtable at runtime.
//
// Unfortunately these functions have to be declared as var not const because the callbacks
// are built at runtime. They are declared globally because `syscall.NewCallback` never
// releases the memory it allocates for the functions thus causing an unsolvable memory
// leak if we were to allocate these per-notification.
//
// The other COM interfaces we are interacting with are auto-generated from metadata.
// However the INotificationActivationCallback is undocumented, so we have to define
// it entirely ourselves.
//
// The definitions are derived from:
//   - <combase.h>
//   - <NotificationActivationCallback.h>
package wintoast

import (
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// Interface GUIDS. These GUIDS are predefined by the Windows Runtime, identifying the various
// interfaces we want to make use of.
var (
	IID_IClassFactory                   = ole.NewGUID("{00000001-0000-0000-C000-000000000046}")
	IID_INotificationActivationCallback = ole.NewGUID("{53E31837-6600-4A81-9395-75CFFE746F94}")
)

// This default GUID is for our implementation.
// This was generated and should not collide with any other GUID.
// It's preferable for the application to override this value with its own generated GUID.
var GUID_ImplNotificationActivationCallback = ole.NewGUID("{0F82E845-CB89-4039-BDBF-67CA33254C76}")

type (
	// IClassFactory defines the factory that builds our INotificationActivationCallback instance.
	// Windows Runtime loves factories.
	IClassFactory struct {
		VTable *IClassFactoryVtbl
	}

	IClassFactoryVtbl struct {
		ole.IUnknownVtbl
		CreateInstance uintptr
		LockServer     uintptr
	}
)

type (
	// INotificationActivationCallback receives activations from toast notifications.
	INotificationActivationCallback struct {
		VTable *INotificationActivationCallbackVtbl
	}

	INotificationActivationCallbackVtbl struct {
		ole.IUnknownVtbl
		Activate uintptr
	}
)

/*
	Strictly speaking we shouldn't need to pin the static objects. They
	are package-globals and wont be garabge collected. No harm in being
	extra careful, though.
*/

var pinner runtime.Pinner

func init() {
	pinner.Pin(ClassFactory)
	pinner.Pin(ClassFactory.VTable)
	pinner.Pin(NotificationActivationCallback)
	pinner.Pin(NotificationActivationCallback.VTable)
}

// Static implementations for the IClassFactory.
var (
	ClassFactory = &IClassFactory{
		VTable: &IClassFactoryVtbl{
			IUnknownVtbl: ole.IUnknownVtbl{
				QueryInterface: IClassFactory_QueryInterface,
				AddRef:         IClassFactory_AddRef,
				Release:        IClassFactory_Release,
			},
			LockServer:     IClassFactory_LockServer,
			CreateInstance: IClassFactory_CreateInstance,
		},
	}

	IClassFactory_AddRef = syscall.NewCallback(func(this *IClassFactory) (re uintptr) {
		return uintptr(1)
	})

	IClassFactory_Release = syscall.NewCallback(func(this *IClassFactory) (re uintptr) {
		return uintptr(1)
	})

	IClassFactory_QueryInterface = syscall.NewCallback(func(this *IClassFactory, riid *ole.GUID, out unsafe.Pointer) (re uintptr) {
		if !ole.IsEqualGUID(riid, IID_IClassFactory) &&
			!ole.IsEqualGUID(riid, ole.IID_IUnknown) {
			return ole.E_NOINTERFACE
		}
		*(**IClassFactory)(out) = this
		return ole.S_OK
	})

	IClassFactory_LockServer = syscall.NewCallback(func(this *IClassFactory, flock uintptr) (ret uintptr) {
		return ole.S_OK
	})

	IClassFactory_CreateInstance = syscall.NewCallback(func(this *IClassFactory, punkOuter *ole.IUnknown, riid *ole.GUID, out unsafe.Pointer) (re uintptr) {
		if punkOuter != nil {
			// Should be CLASS_E_NOAGGREGATION but ole doesn't define this.
			return ole.E_NOINTERFACE
		}
		if !ole.IsEqualGUID(riid, IID_INotificationActivationCallback) &&
			!ole.IsEqualGUID(riid, ole.IID_IUnknown) {
			return ole.E_NOINTERFACE
		}
		*(**INotificationActivationCallback)(out) = NotificationActivationCallback
		return ole.S_OK
	})
)

// Static implementations for the INotificationActivationCallback.
var (
	NotificationActivationCallback = &INotificationActivationCallback{
		VTable: &INotificationActivationCallbackVtbl{
			IUnknownVtbl: ole.IUnknownVtbl{
				QueryInterface: INotificationActivationCallback_QueryInterface,
				AddRef:         INotificationActivationCallback_AddRef,
				Release:        INotificationActivationCallback_Release,
			},
			Activate: INotificationActivationCallback_Activate,
		},
	}

	INotificationActivationCallback_AddRef = syscall.NewCallback(func(this *INotificationActivationCallback) (re uintptr) {
		return uintptr(1)
	})

	INotificationActivationCallback_Release = syscall.NewCallback(func(this *INotificationActivationCallback) (re uintptr) {
		return uintptr(1)
	})

	INotificationActivationCallback_QueryInterface = syscall.NewCallback(func(this *INotificationActivationCallback, riid *ole.GUID, out unsafe.Pointer) (re uintptr) {
		if !ole.IsEqualGUID(riid, IID_INotificationActivationCallback) &&
			!ole.IsEqualGUID(riid, ole.IID_IUnknown) {
			return ole.E_NOINTERFACE
		}
		*(**INotificationActivationCallback)(out) = this
		return ole.S_OK
	})

	// Activate is our re-entrance into Go from Windows. This is the magic.
	INotificationActivationCallback_Activate = syscall.NewCallback(func(
		this unsafe.Pointer,
		appUserModelId unsafe.Pointer,
		invokedArgs unsafe.Pointer,
		data unsafe.Pointer,
		count uint32,
	) (ret uintptr) {
		callback(
			windows.UTF16PtrToString((*uint16)(appUserModelId)),
			windows.UTF16PtrToString((*uint16)(invokedArgs)),
			sliceUserDataFromUnsafe(data, int(count)),
		)
		return
	})
)
