package main

import (
	"errors"
	"log/slog"
	"syscall"

	"reasonix/internal/agent"
)

// Sanitized errors surfaced when a destructive or restore session operation
// hits a real filesystem blocker. Like errSessionBusyElsewhere, they
// intentionally carry no path, so raw OS error text never reaches the UI.
var (
	errSessionFileLocked        = errors.New("a session file is temporarily locked by another program (often antivirus or sync tools) — wait a moment and retry")
	errSessionFileAccessDenied  = errors.New("access to a session file was denied — close programs that may be using it or check folder permissions, then retry")
	errSessionDiskFull          = errors.New("not enough disk space to finish the operation — free some space and retry")
	errSessionHistoryUnreadable = errors.New("session history could not be loaded safely; the session files were left unchanged")
)

// friendlySessionLoadError keeps decoder details and local paths in logs while
// preserving the already path-free replay-budget error for the startup banner.
// A failed authoritative event log must never be hidden by opening an empty
// session or falling back to an older checkpoint.
func friendlySessionLoadError(err error) error {
	if err == nil || errors.Is(err, agent.ErrSessionReplayLimitExceeded) {
		return err
	}
	if friendly := friendlySessionFileError(err); !errors.Is(friendly, err) {
		return friendly
	}
	slog.Warn("desktop: session history load failed", "err", err)
	return errSessionHistoryUnreadable
}

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
	if errors.Is(err, agent.ErrSessionFileLockHeld) {
		return errSessionBusyElsewhere
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
