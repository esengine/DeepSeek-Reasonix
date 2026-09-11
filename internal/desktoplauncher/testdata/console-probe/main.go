// Command console-probe is an independent console executable for launcher tests.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	name := filepath.Base(exe)
	kernel := syscall.NewLazyDLL("kernel32.dll")
	cp, _, _ := kernel.NewProc("GetConsoleCP").Call()
	window, _, _ := kernel.NewProc("GetConsoleWindow").Call()
	fmt.Printf("console-state %s %d %d\n", name, cp, window)
	if name == "reasonix-guard.exe" {
		root := os.Getenv("REASONIX_LAUNCHER_CONSOLE_TEST")
		pointer := `{"schemaVersion":1,"activeVersion":"v1.0.0","activeDir":"versions/v1.0.0"}`
		if err := os.WriteFile(filepath.Join(root, "current.json"), []byte(pointer), 0o644); err != nil {
			panic(err)
		}
	}
}
