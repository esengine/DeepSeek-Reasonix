//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void installQiaotongAgentSystemQuitHook(void);
*/
import "C"

import "sync"

var installSystemQuitHookOnce sync.Once

func installSystemQuitHook() {
	installSystemQuitHookOnce.Do(func() {
		C.installQiaotongAgentSystemQuitHook()
	})
}

//export QiaotongAgentMarkSystemQuit
func QiaotongAgentMarkSystemQuit() {
	markSystemQuitRequested()
}
