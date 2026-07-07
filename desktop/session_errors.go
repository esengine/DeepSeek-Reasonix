package main

import (
	"errors"
	"log/slog"
	"syscall"
)

// Sanitized errors surfaced when a destructive or restore session operation
// hits a real filesystem blocker. Like errSessionBusyElsewhere, they
// intentionally carry no path, so raw OS error text never reaches the UI.
var (
	errSessionFileLocked       = errors.New("a session file is temporarily locked by another program (often antivirus or sync tools) — wait a moment and retry")
	errSessionFileAccessDenied = errors.New("access to a session file was denied — close programs that may be using it or check folder permissions, then retry")
	errSessionDiskFull         = errors.New("not enough disk space to finish the operation — free some space and retry")
)

// friendlySessionFileError rewrites raw OS-level filesystem errors from the
// session trash/restore/purge flows (e.g. a Windows sharing violation while a
// scanner holds a transcript) into the actionable, path-free errors above.
// The original error is logged so diagnostics keep the path and errno.
// Unrecognized errors — including already-sanitized ones like
// errSessionBusyElsewhere — pass through unchanged.
func friendlySessionFileError(err error) error {
	if err == nil {
		return nil
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err
	}
	var friendly error
	switch {
	case isFileInUseErrno(errno):
		friendly = errSessionFileLocked
	case isAccessDeniedErrno(errno):
		friendly = errSessionFileAccessDenied
	case isDiskFullErrno(errno):
		friendly = errSessionDiskFull
	default:
		return err
	}
	slog.Warn("desktop: session file operation blocked", "err", err)
	return friendly
}
