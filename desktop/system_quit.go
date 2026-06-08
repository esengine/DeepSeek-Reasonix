package main

import "sync/atomic"

var systemQuitRequested atomic.Bool

func markSystemQuitRequested() {
	systemQuitRequested.Store(true)
}

func consumeSystemQuitRequested() bool {
	return systemQuitRequested.Swap(false)
}
