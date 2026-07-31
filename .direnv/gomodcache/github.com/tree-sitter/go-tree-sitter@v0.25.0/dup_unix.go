//go:build linux || darwin

package tree_sitter

/*
#include <unistd.h>
*/
import "C"

// Wrapper for Unix systems
func dupeFD(fd uintptr) int {
	return int(C.dup(C.int(fd)))
}
