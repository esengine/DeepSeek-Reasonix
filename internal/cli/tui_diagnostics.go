package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	tuiDiagnosticLogLimit     = 4 << 20
	tuiDiagnosticLogRetention = 7 * 24 * time.Hour
	tuiWatchdogInterval       = time.Second
	tuiWatchdogStall          = 10 * time.Second
)

// tuiDiagnostics owns process-level diagnostics while an interactive terminal
// UI is alive. Bubble Tea owns the terminal screen, so background logs and
// plugin stderr must go to a private file instead of bypassing its renderer.
// Failure to create that file degrades to io.Discard: typed event.Notice values
// still carry user-facing warnings through the TUI.
//
// Milestones and a 1s heartbeat keep the log non-empty during hangs so Windows
// ConPTY freezes (#7435) and stuck D-Bus startups leave recoverable evidence.
type tuiDiagnostics struct {
	previous *slog.Logger
	logger   *slog.Logger
	writer   io.Writer
	file     *os.File
	path     string
	close    sync.Once

	lastProgress atomic.Int64 // unix nano of last Update/View progress
	stopWatch    chan struct{}
	watchOnce    sync.Once
	watchWG      sync.WaitGroup
	killed       atomic.Bool
}

func startTUIDiagnostics(reasonixHome string) *tuiDiagnostics {
	d := &tuiDiagnostics{previous: slog.Default(), writer: io.Discard, stopWatch: make(chan struct{})}
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
	d.markProgress()
	d.Milestone("diagnostics_started")
	return d
}

// Milestone records a startup/runtime phase and flushes the log immediately so a
// subsequent hang still leaves a non-zero diagnostic file.
func (d *tuiDiagnostics) Milestone(name string) {
	if d == nil {
		return
	}
	d.markProgress()
	msg := fmt.Sprintf("milestone=%s t=%s", strings.TrimSpace(name), time.Now().UTC().Format(time.RFC3339Nano))
	_, _ = fmt.Fprintln(d.Writer(), msg)
	d.Sync()
}

// Sync flushes the diagnostic file to disk when possible.
func (d *tuiDiagnostics) Sync() {
	if d == nil || d.file == nil {
		return
	}
	_ = d.file.Sync()
}

// markProgress records that the TUI event loop is alive.
func (d *tuiDiagnostics) markProgress() {
	if d == nil {
		return
	}
	d.lastProgress.Store(time.Now().UnixNano())
}

// Path returns the diagnostic log path (empty when falling back to Discard).
func (d *tuiDiagnostics) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

func (d *tuiDiagnostics) Writer() io.Writer {
	if d == nil || d.writer == nil {
		return io.Discard
	}
	return d.writer
}

// StartWatchdog arms a 1s heartbeat. If the TUI event loop makes no progress for
// 10s, it dumps all goroutines, syncs the log, and kills the Bubble Tea program
// so the terminal is restored instead of remaining frozen (#7435).
func (d *tuiDiagnostics) StartWatchdog(p *tea.Program) {
	if d == nil || p == nil {
		return
	}
	d.watchOnce.Do(func() {
		d.markProgress()
		d.watchWG.Add(1)
		go func() {
			defer d.watchWG.Done()
			d.watch(p)
		}()
	})
}

func (d *tuiDiagnostics) watch(p *tea.Program) {
	ticker := time.NewTicker(tuiWatchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopWatch:
			return
		case now := <-ticker.C:
			last := time.Unix(0, d.lastProgress.Load())
			age := now.Sub(last)
			_, _ = fmt.Fprintf(d.Writer(), "heartbeat t=%s last_progress_age=%s\n",
				now.UTC().Format(time.RFC3339Nano), age.Round(time.Millisecond))
			d.Sync()
			if age < tuiWatchdogStall || d.killed.Load() {
				continue
			}
			d.killed.Store(true)
			d.dumpGoroutines("watchdog_stall")
			d.Sync()
			// Kill restores the terminal; Quit alone can hang if Update is blocked.
			p.Kill()
			return
		}
	}
}

func (d *tuiDiagnostics) dumpGoroutines(reason string) {
	if d == nil {
		return
	}
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, len(buf)*2)
	}
	_, _ = fmt.Fprintf(d.Writer(), "goroutine_dump reason=%s bytes=%d\n%s\n", reason, len(buf), buf)
}

func (d *tuiDiagnostics) Close() {
	if d == nil {
		return
	}
	d.close.Do(func() {
		select {
		case <-d.stopWatch:
		default:
			close(d.stopWatch)
		}
		// Wait for the watchdog to fully exit before closing the log. A timed
		// wait left a window where runtime.Stack / Sync / Kill could still write
		// the file after Close returned.
		d.watchWG.Wait()
		// Do not overwrite a logger deliberately installed by another owner
		// after the TUI started.
		if slog.Default() == d.logger && d.previous != nil {
			slog.SetDefault(d.previous)
		}
		if d.file != nil {
			_ = d.file.Sync()
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
