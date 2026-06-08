//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void installReasonixSystemQuitHook(void);
*/
import "C"

import "sync"

var installSystemQuitHookOnce sync.Once

func installSystemQuitHook() {
	installSystemQuitHookOnce.Do(func() {
		C.installReasonixSystemQuitHook()
	})
}

//export ReasonixMarkSystemQuit
func ReasonixMarkSystemQuit() {
	markSystemQuitRequested()
}
