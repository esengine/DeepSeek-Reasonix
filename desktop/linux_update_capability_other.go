//go:build !linux

package main

func linuxSelfUpdateAvailable() bool { return true }

func linuxSelfUpdateManualReason() string { return "" }
