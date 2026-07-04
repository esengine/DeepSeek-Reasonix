//go:build !darwin

package main

func mainThreadWatchdogSupported() bool {
	return false
}

func startNativeMainThreadHeartbeat(uint64) {}

func stopNativeMainThreadHeartbeat() {}
