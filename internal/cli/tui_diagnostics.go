package cli

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	tuiDiagnosticLogLimit     = 4 << 20
	tuiDiagnosticLogRetention = 7 * 24 * time.Hour
)

// tuiDiagnostics owns process-level diagnostics while an interactive terminal
// UI is alive. Bubble Tea owns the terminal screen, so background logs and
// plugin stderr must go to a private file instead of bypassing its renderer.
// Failure to create that file degrades to io.Discard: typed event.Notice values
// still carry user-facing warnings through the TUI.
type tuiDiagnostics struct {
	previous *slog.Logger
	logger   *slog.Logger
	writer   io.Writer
	file     *os.File
	path     string
	close    sync.Once
}

func startTUIDiagnostics(reasonixHome string) *tuiDiagnostics {
	d := &tuiDiagnostics{previous: slog.Default(), writer: io.Discard}
	if logDir := tuiDiagnosticLogDir(reasonixHome); logDir != "" {
		if err := os.MkdirAll(logDir, 0o700); err == nil {
			pruneTUIDiagnosticLogs(logDir, time.Now())
			if file, err := os.CreateTemp(logDir, "cli-tui-*.log"); err == nil {
				d.file = file
				d.path = file.Name()
				d.writer = &boundedDiagnosticWriter{dst: file, remaining: tuiDiagnosticLogLimit}
			}
		}
	}
	d.logger = slog.New(slog.NewTextHandler(d.writer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(d.logger)
	return d
}

func (d *tuiDiagnostics) Writer() io.Writer {
	if d == nil || d.writer == nil {
		return io.Discard
	}
	return d.writer
}

func (d *tuiDiagnostics) Close() {
	if d == nil {
		return
	}
	d.close.Do(func() {
		// Do not overwrite a logger deliberately installed by another owner
		// after the TUI started.
		if slog.Default() == d.logger && d.previous != nil {
			slog.SetDefault(d.previous)
		}
		if d.file != nil {
			_ = d.file.Close()
		}
	})
}

func tuiDiagnosticLogDir(reasonixHome string) string {
	if strings.TrimSpace(reasonixHome) == "" {
		return ""
	}
	return filepath.Join(reasonixHome, "logs")
}

func pruneTUIDiagnosticLogs(logDir string, now time.Time) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	cutoff := now.Add(-tuiDiagnosticLogRetention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "cli-tui-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(logDir, entry.Name()))
	}
}

type boundedDiagnosticWriter struct {
	mu        sync.Mutex
	dst       io.Writer
	remaining int64
	truncated bool
}

func (w *boundedDiagnosticWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	total := len(p)
	if total == 0 || w.dst == nil || w.remaining <= 0 {
		return total, nil
	}
	n := total
	if int64(n) > w.remaining {
		n = int(w.remaining)
	}
	written, err := w.dst.Write(p[:n])
	if written > 0 {
		w.remaining -= int64(written)
	}
	if err != nil || written != n {
		w.remaining = 0
		return total, nil
	}
	if n < total && !w.truncated {
		w.truncated = true
		_, _ = io.WriteString(w.dst, "\nreasonix: CLI TUI diagnostic log limit reached; further diagnostics omitted\n")
		w.remaining = 0
	}
	return total, nil
}
