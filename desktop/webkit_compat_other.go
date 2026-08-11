//go:build !linux || !cgo

package main

func scheduleWebKitSignalHandlerRepair() {}

func repairWebKitSignalHandlers() {}
