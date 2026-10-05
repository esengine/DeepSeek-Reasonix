//go:build !windows && !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "reasonix-update-helper is only used by Windows and Linux desktop builds")
	os.Exit(2)
}
