//go:build windows && reasonix_transcript_smoke

package edge

import (
	"fmt"
	"io"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/internal/w32"
	"golang.org/x/sys/windows"
)

const coreWebView2CapturePreviewImageFormatPNG = 0

type smokeIStreamVtbl struct {
	_IUnknownVtbl
	Read         ComProc
	Write        ComProc
	Seek         ComProc
	SetSize      ComProc
	CopyTo       ComProc
	Commit       ComProc
	Revert       ComProc
	LockRegion   ComProc
	UnlockRegion ComProc
	Stat         ComProc
	Clone        ComProc
}

type smokeIStream struct {
	vtbl *smokeIStreamVtbl
}

func (stream *smokeIStream) seekStart() error {
	hr, _, _ := stream.vtbl.Seek.Call(
		uintptr(unsafe.Pointer(stream)),
		0,
		0,
		0,
	)
	if windows.Handle(hr) != windows.S_OK {
		return syscall.Errno(hr)
	}
	return nil
}

type capturePreviewCompletedHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type capturePreviewCompletedHandler struct {
	vtbl *capturePreviewCompletedHandlerVtbl
	done chan error
}

func capturePreviewQueryInterface(_ *capturePreviewCompletedHandler, _, _ uintptr) uintptr { return 0 }
func capturePreviewAddRef(_ *capturePreviewCompletedHandler) uintptr                       { return 1 }
func capturePreviewRelease(_ *capturePreviewCompletedHandler) uintptr                      { return 1 }
func capturePreviewInvoke(handler *capturePreviewCompletedHandler, errorCode uintptr) uintptr {
	var err error
	if windows.Handle(errorCode) != windows.S_OK {
		err = syscall.Errno(errorCode)
	}
	select {
	case handler.done <- err:
	default:
	}
	return 0
}

var capturePreviewCompletedHandlerMethods = capturePreviewCompletedHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(capturePreviewQueryInterface),
		AddRef:         NewComProc(capturePreviewAddRef),
		Release:        NewComProc(capturePreviewRelease),
	},
	Invoke: NewComProc(capturePreviewInvoke),
}

// SmokePreviewCapture owns the COM stream until WebView2 finishes producing a
// compositor PNG. It only exists in the reasonix_transcript_smoke build and is
// polled by the native host while that host keeps the STA message loop moving.
type SmokePreviewCapture struct {
	stream  *IStream
	handler *capturePreviewCompletedHandler
	done    chan error
	closed  bool
}

// StartSmokePreviewCapture starts a compositor capture without blocking the
// WebView2 STA. Call Poll while pumping the native message loop.
func (e *Chromium) StartSmokePreviewCapture() (*SmokePreviewCapture, error) {
	if e.webview == nil || e.shuttingDown {
		return nil, fmt.Errorf("WebView2 is not ready for preview capture")
	}
	streamPointer, err := w32.SHCreateMemStream([]byte{0})
	if err != nil {
		return nil, fmt.Errorf("create preview memory stream: %w", err)
	}
	stream := (*IStream)(unsafe.Pointer(streamPointer))
	done := make(chan error, 1)
	handler := &capturePreviewCompletedHandler{
		vtbl: &capturePreviewCompletedHandlerMethods,
		done: done,
	}
	hr, _, _ := e.webview.vtbl.CapturePreview.Call(
		uintptr(unsafe.Pointer(e.webview)),
		coreWebView2CapturePreviewImageFormatPNG,
		uintptr(unsafe.Pointer(stream)),
		uintptr(unsafe.Pointer(handler)),
	)
	if windows.Handle(hr) != windows.S_OK {
		_ = stream.Release()
		return nil, fmt.Errorf("start WebView2 preview capture: %w", syscall.Errno(hr))
	}
	return &SmokePreviewCapture{stream: stream, handler: handler, done: done}, nil
}

// Poll returns done=false until the asynchronous CapturePreview callback runs.
// On completion it rewinds and drains the PNG stream exactly once.
func (capture *SmokePreviewCapture) Poll() (png []byte, done bool, err error) {
	if capture.closed {
		return nil, true, fmt.Errorf("preview capture is already closed")
	}
	select {
	case captureErr := <-capture.done:
		if captureErr != nil {
			capture.close()
			return nil, true, captureErr
		}
		stream := (*smokeIStream)(unsafe.Pointer(capture.stream))
		if err := stream.seekStart(); err != nil {
			capture.close()
			return nil, true, fmt.Errorf("rewind preview stream: %w", err)
		}
		data, readErr := io.ReadAll(capture.stream)
		capture.close()
		if readErr != nil {
			return nil, true, fmt.Errorf("read preview stream: %w", readErr)
		}
		if len(data) == 0 {
			return nil, true, fmt.Errorf("WebView2 preview capture returned an empty PNG")
		}
		runtime.KeepAlive(capture.handler)
		return data, true, nil
	default:
		return nil, false, nil
	}
}

func (capture *SmokePreviewCapture) close() {
	if capture.closed {
		return
	}
	capture.closed = true
	_ = capture.stream.Release()
}

// SmokeRuntimeVersion reports the actual installed runtime used by the host.
func (e *Chromium) SmokeRuntimeVersion() string {
	return e.webview2RuntimeVersion
}
