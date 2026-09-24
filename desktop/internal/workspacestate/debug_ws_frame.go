//go:build debugpprof

package workspacestate

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// deepestConflictFrame names the innermost lifecycle.go frame on the stack, so a
// conflict raised by a guard inside commitOperation or PrepareOperationContent is
// attributed to that guard instead of to the Store method that called mutate.
func deepestConflictFrame() string {
	pc := make([]uintptr, 64)
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])
	chain := []string{}
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.File, "lifecycle.go") || strings.HasSuffix(frame.File, "store.go") {
			chain = append(chain, filepath.Base(frame.File)+":"+strconv.Itoa(frame.Line))
		}
		if !more {
			break
		}
	}
	return strings.Join(chain, "<-")
}
