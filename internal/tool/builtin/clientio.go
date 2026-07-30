package builtin

import (
	"context"
	"time"
)

// FileOverlay lets a host transport (an ACP client editor, say) serve file
// content instead of the local disk, so tools see unsaved editor buffers. A
// nil overlay or an ok=false answer falls back to direct disk I/O; the overlay
// is consulted only after the tool's own path resolution and confinement
// checks, so it never widens what a tool may touch.
type FileOverlay interface {
	// ReadTextFile returns the current text of path as the host sees it
	// (including unsaved changes). ok=false means the host cannot serve this
	// path and the caller should read the local disk instead.
	ReadTextFile(ctx context.Context, path string) (content string, ok bool)
	// WriteTextFile asks the host to write content to path (updating any open
	// buffer as well as the file). ok=false means the host cannot handle the
	// write and the caller should write the local disk instead; err is only
	// meaningful when ok is true.
	WriteTextFile(ctx context.Context, path, content string) (ok bool, err error)
}

// TerminalRunner lets a host transport run a foreground shell command in a
// host-owned terminal (the ACP terminal/* methods, say) so the user watches it
// live. ok=false means the host cannot run it and the caller should execute
// locally; err is only meaningful when ok is true. Runners are only consulted
// when the local OS sandbox is not enforcing — a host terminal cannot honor
// the local confinement configuration.
type TerminalRunner interface {
	RunCommand(ctx context.Context, command, cwd string, timeout time.Duration) (output string, ok bool, err error)
}
