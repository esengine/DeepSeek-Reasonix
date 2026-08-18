//go:build windows

package edge

import "sync"

// WindowStateDiagnostic contains dimensions only. It lets the desktop correlate
// mixed-DPI failures without collecting window titles or page content.
type WindowStateDiagnostic struct {
	ClientRect Rect
	WindowRect Rect
	DPI        uint32
	Scale      float64
}

var windowStateObserver struct {
	sync.RWMutex
	callback func(WindowStateDiagnostic)
}

func SetWindowStateObserver(callback func(WindowStateDiagnostic)) {
	windowStateObserver.Lock()
	windowStateObserver.callback = callback
	windowStateObserver.Unlock()
}

func notifyWindowStateObserver(diagnostic WindowStateDiagnostic) {
	windowStateObserver.RLock()
	callback := windowStateObserver.callback
	windowStateObserver.RUnlock()
	if callback != nil {
		callback(diagnostic)
	}
}
