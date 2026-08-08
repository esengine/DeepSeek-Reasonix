//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Cocoa
int reasonixAppIsActive(void);
*/
import "C"

func windowIsFrontmost() bool {
	return C.reasonixAppIsActive() != 0
}
