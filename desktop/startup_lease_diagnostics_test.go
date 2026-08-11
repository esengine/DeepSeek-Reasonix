package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestSetTabStartupErrorLogsLeaseHolderDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	path := filepath.Join(t.TempDir(), "issue-8372.jsonl")
	tab := &WorkspaceTab{
		ID:              "tab-8372",
		SessionPath:     path,
		buildGeneration: 7,
		runtimeID:       "runtime-8372",
	}
	acquiredAt := time.Date(2026, time.August, 11, 7, 54, 51, 123, time.UTC)
	err := &agent.SessionLeaseError{
		Path: path,
		Info: &agent.SessionLeaseInfo{
			SessionPath: path,
			WriterID:    agent.SessionWriterID(),
			PID:         os.Getpid(),
			Hostname:    "test-host",
			AcquiredAt:  acquiredAt,
		},
	}

	if !setTabStartupError(tab, err) {
		t.Fatal("setTabStartupError did not classify the lease error as retryable")
	}
	if strings.Contains(tab.StartupErr, path) || strings.Contains(tab.StartupErr, agent.SessionWriterID()) {
		t.Fatalf("user-facing startup error leaked lease diagnostics: %q", tab.StartupErr)
	}

	logLine := output.String()
	for _, want := range []string{
		"desktop: session startup lease blocked",
		"tab=tab-8372",
		"build_generation=7",
		"runtime_id=runtime-8372",
		"session_path=" + path,
		"session_runtime_key=" + sessionRuntimeKey(path),
		"holder_scope=current_process",
		"holder_pid=" + strconv.Itoa(os.Getpid()),
		"holder_host=test-host",
		"holder_writer_id=" + agent.SessionWriterID(),
		"holder_acquired_at=" + acquiredAt.Format(time.RFC3339Nano),
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("diagnostic log %q does not contain %q", logLine, want)
		}
	}
}
