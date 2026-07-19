//go:build !darwin

package main

import (
	"fmt"
	"runtime"
)

func applyMac(string, string) error {
	return fmt.Errorf("self-update unsupported on %s", runtime.GOOS)
}
