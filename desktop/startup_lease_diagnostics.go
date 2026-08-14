package main

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"reasonix/internal/agent"
)

func setTabStartupError(tab *WorkspaceTab, err error) bool {
	if tab == nil {
		return false
	}
	tab.StartupErr = userFacingSessionLeaseError("", err).Error()
	tab.StartupErrLeaseHeld = errors.Is(err, agent.ErrSessionLeaseHeld)
	if tab.StartupErrLeaseHeld {
		logTabStartupLeaseDiagnostics(tab, err)
	}
	return tab.StartupErrLeaseHeld
}

func logTabStartupLeaseDiagnostics(tab *WorkspaceTab, err error) {
	if tab == nil || !errors.Is(err, agent.ErrSessionLeaseHeld) {
		return
	}
	path := strings.TrimSpace(tab.currentSessionPath())
	holderScope := "unknown"
	holderPID := 0
	holderHost := ""
	holderWriterID := ""
	holderAcquiredAt := ""
	var leaseErr *agent.SessionLeaseError
	if errors.As(err, &leaseErr) && leaseErr != nil {
		if strings.TrimSpace(leaseErr.Path) != "" {
			path = leaseErr.Path
		}
		if leaseErr.Info != nil {
			holderPID = leaseErr.Info.PID
			holderHost = strings.TrimSpace(leaseErr.Info.Hostname)
			holderWriterID = strings.TrimSpace(leaseErr.Info.WriterID)
			if !leaseErr.Info.AcquiredAt.IsZero() {
				holderAcquiredAt = leaseErr.Info.AcquiredAt.UTC().Format(time.RFC3339Nano)
			}
			if holderPID == os.Getpid() && holderWriterID == agent.SessionWriterID() {
				holderScope = "current_process"
			} else {
				holderScope = "external_process"
			}
		}
	}
	slog.Warn(
		"desktop: session startup lease blocked",
		"tab", tab.ID,
		"build_generation", tab.buildGeneration,
		"runtime_id", tab.runtimeID,
		"session_path", path,
		"session_runtime_key", sessionRuntimeKey(path),
		"holder_scope", holderScope,
		"holder_pid", holderPID,
		"holder_host", holderHost,
		"holder_writer_id", holderWriterID,
		"holder_acquired_at", holderAcquiredAt,
	)
}
