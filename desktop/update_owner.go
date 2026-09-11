package main

import "os"

func windowsUpdateOwnerPID() int {
	if hostRPCRequested(os.Args[1:]) {
		return os.Getppid()
	}
	return os.Getpid()
}
