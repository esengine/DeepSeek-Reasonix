//go:build darwin

package main

/*
#cgo darwin CFLAGS: -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>

int ReasonixEmbedAvailable(void);
const char* ReasonixEmbedEngineName(void);
void ReasonixEmbedShow(void);
void ReasonixEmbedHide(void);
void ReasonixEmbedDestroy(void);
void ReasonixEmbedSetBounds(double x, double y, double w, double h);
void ReasonixEmbedNavigate(const char* url);
void ReasonixEmbedReload(void);
void ReasonixEmbedGoBack(void);
void ReasonixEmbedGoForward(void);
void ReasonixEmbedSetZoom(double factor);
void ReasonixEmbedSnapshotPNGAsync(void);
void ReasonixEmbedSetPickMode(int enabled, const char* accent, const char* accentFg);
*/
import "C"

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var (
	snapshotMu      sync.Mutex
	snapshotWaiters []chan snapshotResult
)

type snapshotResult struct {
	data string
	err  error
}

func platformEmbedAvailable() bool {
	return C.ReasonixEmbedAvailable() != 0
}

func platformEmbedEngineName() string {
	return C.GoString(C.ReasonixEmbedEngineName())
}

func platformEmbedShow() error {
	C.ReasonixEmbedShow()
	return nil
}

func platformEmbedHide() {
	C.ReasonixEmbedHide()
}

func platformEmbedDestroy() {
	C.ReasonixEmbedDestroy()
}

func platformEmbedSetBounds(b EmbedBrowserBounds) {
	C.ReasonixEmbedSetBounds(C.double(b.X), C.double(b.Y), C.double(b.Width), C.double(b.Height))
}

func platformEmbedNavigate(raw string) error {
	cURL := C.CString(raw)
	defer C.free(unsafe.Pointer(cURL))
	C.ReasonixEmbedNavigate(cURL)
	return nil
}

func platformEmbedReload() {
	C.ReasonixEmbedReload()
}

func platformEmbedGoBack() {
	C.ReasonixEmbedGoBack()
}

func platformEmbedGoForward() {
	C.ReasonixEmbedGoForward()
}

func platformEmbedSetZoom(factor float64) {
	C.ReasonixEmbedSetZoom(C.double(factor))
}

func platformEmbedSnapshotPNG() (string, error) {
	ch := make(chan snapshotResult, 1)
	snapshotMu.Lock()
	snapshotWaiters = append(snapshotWaiters, ch)
	snapshotMu.Unlock()

	C.ReasonixEmbedSnapshotPNGAsync()

	select {
	case r := <-ch:
		return r.data, r.err
	case <-time.After(8 * time.Second):
		snapshotMu.Lock()
		// Drop this waiter if still present.
		for i, w := range snapshotWaiters {
			if w == ch {
				snapshotWaiters = append(snapshotWaiters[:i], snapshotWaiters[i+1:]...)
				break
			}
		}
		snapshotMu.Unlock()
		return "", errors.New("截图超时")
	}
}

func platformEmbedSetPickMode(enabled bool, accent, accentFg string) {
	flag := C.int(0)
	if enabled {
		flag = 1
	}
	cAccent := C.CString(strings.TrimSpace(accent))
	cAccentFg := C.CString(strings.TrimSpace(accentFg))
	defer C.free(unsafe.Pointer(cAccent))
	defer C.free(unsafe.Pointer(cAccentFg))
	C.ReasonixEmbedSetPickMode(flag, cAccent, cAccentFg)
}

//export ReasonixEmbedBrowserEmitState
func ReasonixEmbedBrowserEmitState(url, title *C.char, canBack, canForward, loading C.int) {
	// Copy out of C memory, then leave the CGO/AppKit call stack before emitting.
	state := EmbedBrowserState{
		URL:          C.GoString(url),
		Title:        C.GoString(title),
		CanGoBack:    canBack != 0,
		CanGoForward: canForward != 0,
		Loading:      loading != 0,
		Engine:       "webkit",
	}
	go emitEmbedBrowserState(state)
}

//export ReasonixEmbedBrowserEmitError
func ReasonixEmbedBrowserEmitError(message *C.char) {
	msg := strings.TrimSpace(C.GoString(message))
	if msg == "" {
		return
	}
	go emitEmbedBrowserError(msg)
}

//export ReasonixEmbedBrowserEmitPick
func ReasonixEmbedBrowserEmitPick(x, y, w, h C.double, selector, tagName, text *C.char) {
	pick := EmbedBrowserPick{
		X:        float64(x),
		Y:        float64(y),
		Width:    float64(w),
		Height:   float64(h),
		Selector: C.GoString(selector),
		TagName:  C.GoString(tagName),
		Text:     C.GoString(text),
	}
	go emitEmbedBrowserPick(pick)
}

//export ReasonixEmbedBrowserSnapshotDone
func ReasonixEmbedBrowserSnapshotDone(dataURLOrError *C.char) {
	s := C.GoString(dataURLOrError)
	var r snapshotResult
	if strings.HasPrefix(s, "error:") {
		r.err = errors.New(strings.TrimPrefix(s, "error:"))
	} else {
		r.data = s
	}
	go func(result snapshotResult) {
		snapshotMu.Lock()
		waiters := snapshotWaiters
		snapshotWaiters = nil
		snapshotMu.Unlock()
		for _, ch := range waiters {
			select {
			case ch <- result:
			default:
			}
		}
	}(r)
}

func emitEmbedBrowserState(state EmbedBrowserState) {
	if state.Engine == "" {
		state.Engine = platformEmbedEngineName()
	}
	embedMu.Lock()
	a := embedApp
	embedMu.Unlock()
	if a == nil || a.ctx == nil {
		return
	}
	// Never call Wails EventsEmit synchronously from WebKit/AppKit callbacks —
	// it can block the main thread when the webview event channel backs up.
	a.runtimeEvents.Emit(a.ctx, "embed-browser:state", state)
}

func emitEmbedBrowserPick(pick EmbedBrowserPick) {
	embedMu.Lock()
	a := embedApp
	embedMu.Unlock()
	if a == nil || a.ctx == nil {
		return
	}
	a.runtimeEvents.Emit(a.ctx, "embed-browser:pick", pick)
}
