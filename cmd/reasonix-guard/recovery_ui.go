package main

type recoveryChoice int

const (
	recoveryQuit recoveryChoice = iota
	recoverySafeMode
	recoveryRepair
)
