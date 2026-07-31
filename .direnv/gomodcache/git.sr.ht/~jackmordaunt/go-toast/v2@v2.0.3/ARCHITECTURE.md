# Architecture

This document will attempt to explain how this code works. 

## Windows COM 

Windows makes heavy use of it's [COM api](https://en.wikipedia.org/wiki/Component_Object_Model), 
(Component Object Model) which is a binary interface - allowing programs that agree on a memory 
layout in order to communicate. 

COM apis are typically Object Oriented, and based on interfaces. This should be familiar to Go
programmers, since Go includes interfaces as a core part of its language design and type system.  

The difference being that in COM we don't get any runtime help, nice syntax or type safety. We
get raw [VTables](https://en.wikipedia.org/wiki/Virtual_method_table) and deal with raw memory. 

You can think of COM as like working with a Go api that uses `any` (empty interface) _everywhere_
and typeswitching is required to access methods `file, ok := obj.(File)`.  

Some languages like C++ have extensions that support COM and provide convenient wrappers for
generating and using COM apis. Go does not. C also, does not. 

However there is a package `go-ole` that allows us to _call_ COM apis with some level of 
convenience - which we will use where possible. What go-ole does not expose is a way to 
implement a COM object in Go. 

## Interacting with COM objects in pure Go

In order to interact with COM objects we need to:

1. locate headers containing the VTable definitions
2. define vtables in Go that are compatable with those definitions
3. invoke the appropriate COM objects using our vtables and the syscall package  

### 1. locate headers 

Download Windows SDK via the [Visual Studio installer](https://visualstudio.microsoft.com/downloads). 
You will need to check "Desktop development with C++".

Once complete you can navigate to the SDK include directory.

In our case we needed `Windows.ui.notifications.h`, which contains the definitions of
the types we want to call, and `NotificationActivationCallback.h` which contains the definition of
`INotificationActivationCallback` which is the interface we need to _implement_.

### 2. define vtables in Go

The VTables are defined in C (mired in macros). We need to define compatible vtables in Go syntax
so we can call the ones defined in the header. 

COM objects are structured in a such a way that we want a parent struct who's first field is a pointer
to the vtable struct. A full example is provided later, for now it we need something like this:

```go
type Object struct {
  lpvtbl *ObjectVtbl
}
type ObjectVtbl struct {
  MethodOne uintptr
  MethdoTwo uintptr
  //...
}
```

### 3. invoke methods in Go 

Using package `syscall` we can invoke these methods (provided the uintptr are valid) using
`syscal.SyscallN`. Paramters and return values are defined in the C headers. 

```go
func (v *Object) One() error {
  hr, _, _ := syscall.SyscallN(uintptr(v))
  if hr != ole.S_OK {
    return ole.NewError(hr)
  }
  return nil
}
```

With that we can inoke methods on a COM object. This is how `go-ole` works. 

## Implementing a COM object in pure Go (no cgo!)

To do this we will need to allocate raw memory for the VTables (so that Go garbage collector
doesn't interfere) and write our function pointers to the VTables. 

Since these are not safe Go capabilities we will need the help of package `syscall` (on Windows). 

Package `syscall` provides two very important functions:

1. `NewProc` - which loads a function from a DLL 
2. `NewCallback` - which allocates a C-callable function pointer from a Go function

For the first part, we can load the Windows kernel api via `kernel32.dll` system dll, and
pull out `GlobalAlloc` and `GlobalFree` using `syscall.NewProc`. 

For the second part, we can use `syscall.NewCallback` to build a C-callable function pointer 
from a Go function and instantiate the VtTables with it. Caveat emptor: memory allocated by
`NewCallback` is never released, and only 1024 callbacks are guaranteed to be allowed. This
is why we only allocate the callbacks once on init.

Thus we can implement a COM object (invokable from C) like this:

```go

// Initialize our kernel functions. 
var (
  kernel32   = windows.NewLazySystemDLL("kernel32.dll")
  procMalloc = kernel32.NewProc("GlobalAlloc")
  procFree   = kernel32.NewProc("GlobalFree")
)

// malloc allocates raw memory using the Windows kernel.
// In case of out of memory, the returned pointer will be nil.
// The memory is zeroed out to make sure we don't get garbage that looks like
// valid Go data types.
func malloc(size uintptr) unsafe.Pointer {
	hr, _, _ := procMalloc.Call(uintptr(GMEM_FIXED|GMEM_ZEROINIT), uintptr(size))
	if hr == 0 {
		return nil
	}
	return unsafe.Pointer(hr)
}

// free deallocates raw memory allocated by malloc.
func free(object unsafe.Pointer) {
	procFree.Call(uintptr(object))
}

// Object defines our object. 
// This is how COM objects are laid out in memory, where the first field is a pointer
// to a vtable, and the vtable's fields are pointers to functions. 
type Object struct {
  lpvtbl *ObjectVtbl // lpvtbl is a COM conventional name for this field. 
}

// ObjectVtbl defines the Vtable of our object. 
type ObjectVtbl struct {
  MethodOne   uintptr
  MethodTwo   uintptr
  MethodThree uintptr
}

// These methods are allocated once as package globals because Go will never reclaim the
// memory allocated for such callbacks. 
// 
// All arguments must be uintptr sized, and the return must be a uintptr as well. 
// 
// By convention, the first parameter is a pointer to the parent object. 
var (
  methodOne = syscall.NewCallback(func(this *Object) uintptr {
    fmt.Printf("methodOne invoked\n")
    return uintptr(0)
  })
  
  methodTwo = syscall.NewCallback(func(this *Object) uintptr {
    fmt.Printf("methodTwo invoked\n")
    return uintptr(0)
  })
  
  methodThree = syscall.NewCallback(func(this *Object) uintptr {
    fmt.Printf("methodThree invoked\n")
    return uintptr(0)
  })
)


func NewObject() *Object {
  // Allocate the parent object and the vtable. 
  obj := (*Object)(malloc(unsafe.Sizeof(Object{})))
  vtbl := (*ObjectVtbl)(malloc(unsafe.Sizeof(ObjectVtbl{})))

  // Initialize the vtable with our static callback implementations. 
  vtbl.MethodOne = methodOne
  vtbl.MethodTwo = methodTwo
  vtbl.MethodThree = methodThree
  
  // The returned object must be freed by GlobalFree. 
  object.lpvtbl = vtbl
  return obj
}
```

## WinRT and Toast Notifications

For this package the vtables we need are located in various headers `Windows.ui.notifications.h` and
`NotificationActivationCallback.h` and `combase.h`.

With all of the vtables replicated in Go as explained above we now need to interact with the Windows
Runtime. 

First we need to initialize the Windows Runtime with `RoInitialize`. 

```go
ole.RoInitialize(0)
```

Traditional COM uses GUIDs to identify objects and interfaces. WinRT uses strings (mapped to GUIDS 
at runtime).

To instantiate a WinRT COM object we invoke `RoGetActivationFactory` with the class string along with
the interface GUID we expect to use. 


```go
CLSID_ToastNotification := "Windows.UI.Notifications.ToastNotification"
IID_IToastNotificationFactory := ole.NewGUID("{50AC103F-D235-4598-BBEF-98FE4D1A3AD4}")

factoryObject, err := ole.RoGetActivationFactory(CLSID_ToastNotification, IID_ToastNotificationFactory)
if err != nil {
	return nil, fmt.Errorf("getting activation factory: %w", err)
}
```

From there we can unsafe cast to our callback definition (ole doesn't provide direct access to the methods).

```go
factory := (*IToastNotificationFactory)(unsafe.Pointer(factoryObject))
notification, err := factory.CreateToastNotification(xml)
```

Repeat this process per object we need to instantiate. 

To generate a toast notification from XML with a callback we need to instantiate several COM objects:

1. `INotificationActivationCallback` our implementation to be invoked by the runtime 
1. `ClassFactory` which can instantiate our `INotificationActivationCallback` implementation
1. `XmlDocument` to contain the xml content of the notification
1. `XmlDocumentIO` to provide an IO interface to the xml document (so we can write the xml to it)
1. `Notification` specifying the content of the notification
1. `Notifier` for showing notifications

Finally we register our class factory using `CoRegisterClassObject` so the runtime can call us back
and then we invoke `Notifier.Show` passing in the `Notification` object to display the notification. 

In addition to calling and implementing COM objects we need to manipulate registry state to tell the 
Windows Runtime metadata about our application. 

1. register a CLSID (GUID) for our INotificationActivationCallback; this is how the runtime knows
what object to ask for
2. optionally provide an icon and an activation executable to be invoked when our application is not running


With all of that correctly configured we can generate toast notifications on Windows in pure Go! 