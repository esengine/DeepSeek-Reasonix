//go:build windows

package edge

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCollectProcessFailedDiagnosticFallsBackOnENoInterface(t *testing.T) {
	args := &ICoreWebView2ProcessFailedEventArgs{vtbl: &_ICoreWebView2ProcessFailedEventArgsVtbl{
		_IUnknownVtbl: _IUnknownVtbl{
			QueryInterface: NewComProc(func(_, _, _ uintptr) uintptr {
				return uintptr(windows.E_NOINTERFACE)
			}),
		},
		GetProcessFailedKind: NewComProc(func(_, result uintptr) uintptr {
			*(*COREWEBVIEW2_PROCESS_FAILED_KIND)(unsafe.Pointer(result)) = COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED
			return uintptr(windows.S_OK)
		}),
	}}

	diagnostic := collectProcessFailedDiagnostic(args)
	if diagnostic.Kind != COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED {
		t.Fatalf("kind = %v", diagnostic.Kind)
	}
	if diagnostic.ReasonAvailable || diagnostic.ExitCodeAvailable || diagnostic.ProcessDescription != "" || diagnostic.FailureSourceModule != "" {
		t.Fatalf("unsupported optional interfaces leaked fields: %+v", diagnostic)
	}
}
