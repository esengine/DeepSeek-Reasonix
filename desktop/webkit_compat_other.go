//go:build !linux

package main

func configureWebKitRendererRecovery(bool) {}

func scheduleWebKitSignalHandlerRepair() {}

func repairWebKitSignalHandlers() {}
