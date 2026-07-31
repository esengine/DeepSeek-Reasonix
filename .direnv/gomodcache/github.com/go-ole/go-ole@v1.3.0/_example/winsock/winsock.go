// +build windows

package main

import (
	"log"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

type EventReceiver struct {
	lpVtbl *EventReceiverVtbl
	ref    int32
	host   *ole.IDispatch
}

type EventReceiverVtbl struct {
	pQueryInterface   uintptr
	pAddRef           uintptr
	pRelease          uintptr
	pGetTypeInfoCount uintptr
	pGetTypeInfo      uintptr
	pGetIDsOfNames    uintptr
	pInvoke           uintptr
}

func QueryInterface(this *ole.IUnknown, iid *ole.GUID, punk **ole.IUnknown) uint32 {
	s, _ := ole.StringFromCLSID(iid)
	*punk = nil
	if ole.IsEqualGUID(iid, ole.IID_IUnknown) ||
		ole.IsEqualGUID(iid, ole.IID_IDispatch) {
		AddRef(this)
		*punk = this
		return ole.S_OK
	}
	if s == "{248DD893-BB45-11CF-9ABC-0080C7E7B78D}" {
		AddRef(this)
		*punk = this
		return ole.S_OK
	}
	return ole.E_NOINTERFACE
}

func AddRef(this *ole.IUnknown) int32 {
	pthis := (*EventReceiver)(unsafe.Pointer(this))
	pthis.ref++
	return pthis.ref
}

func Release(this *ole.IUnknown) int32 {
	pthis := (*EventReceiver)(unsafe.Pointer(this))
	pthis.ref--
	return pthis.ref
}

func GetIDsOfNames(this *ole.IUnknown, iid *ole.GUID, wnames []*uint16, namelen int, lcid int, pdisp []int32) uintptr {
	for n := 0; n < namelen; n++ {
		pdisp[n] = int32(n)
	}
	return uintptr(ole.S_OK)
}

func GetTypeInfoCount(pcount *int) uintptr {
	if pcount != nil {
		*pcount = 0
	}
	return uintptr(ole.S_OK)
}

func GetTypeInfo(ptypeif *uintptr) uintptr {
	return uintptr(ole.E_NOTIMPL)
}

func Invoke(this *ole.IDispatch, dispid int, riid *ole.GUID, lcid int, flags int16, dispparams *ole.DISPPARAMS, result *ole.VARIANT, pexcepinfo *ole.EXCEPINFO, nerr *uint) uintptr {
	switch dispid {
	case 0:
		log.Println("DataArrival")
		winsock := (*EventReceiver)(unsafe.Pointer(this)).host
		var data ole.VARIANT
		ole.VariantInit(&data)
		oleutil.CallMethod(winsock, "GetData", &data)
		s := string(data.ToArray().ToByteArray())
		println()
		println(s)
		println()
	case 1:
		log.Println("Connected")
		winsock := (*EventReceiver)(unsafe.Pointer(this)).host
		oleutil.CallMethod(winsock, "SendData", "GET / HTTP/1.0\r\n\r\n")
	case 3:
		log.Println("SendProgress")
	case 4:
		log.Println("SendComplete")
	case 5:
		log.Println("Close")
		this.Release()
	case 6:
		log.Fatal("Error")
	default:
		log.Println(dispid)
	}
	return ole.E_NOTIMPL
}

func main() {
	ole.CoInitialize(0)

	unknown, err := oleutil.CreateObject("{248DD896-BB45-11CF-9ABC-0080C7E7B78D}")
	if err != nil {
		panic(err.Error())
	}
	winsock, _ := unknown.QueryInterface(ole.IID_IDispatch)
	iid, _ := ole.CLSIDFromString("{248DD893-BB45-11CF-9ABC-0080C7E7B78D}")

	dest := &EventReceiver{}
	dest.lpVtbl = &EventReceiverVtbl{}
	dest.lpVtbl.pQueryInterface = syscall.NewCallback(QueryInterface)
	dest.lpVtbl.pAddRef = syscall.NewCallback(AddRef)
	dest.lpVtbl.pRelease = syscall.NewCallback(Release)
	dest.lpVtbl.pGetTypeInfoCount = syscall.NewCallback(GetTypeInfoCount)
	dest.lpVtbl.pGetTypeInfo = syscall.NewCallback(GetTypeInfo)
	dest.lpVtbl.pGetIDsOfNames = syscall.NewCallback(GetIDsOfNames)
	dest.lpVtbl.pInvoke = syscall.NewCallback(Invoke)
	dest.host = winsock

	oleutil.ConnectObject(winsock, iid, (*ole.IUnknown)(unsafe.Pointer(dest)))
	_, err = oleutil.CallMethod(winsock, "Connect", "127.0.0.1", 80)
	if err != nil {
		log.Fatal(err)
	}

	var m ole.Msg
	for dest.ref != 0 {
		ole.GetMessage(&m, 0, 0, 0)
		ole.DispatchMessage(&m)
	}
}
