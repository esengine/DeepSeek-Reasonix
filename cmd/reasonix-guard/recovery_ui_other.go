//go:build !darwin && !windows && !linux

package main

func nativeRecoveryChoice() recoveryChoice { return recoverySafeMode }
