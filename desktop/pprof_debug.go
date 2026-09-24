//go:build debugpprof

// Local-only diagnostic endpoint: build with `-tags debugpprof` and set
// REASONIX_PPROF=1 to expose net/http/pprof on 127.0.0.1:6060. Never ships.
package main

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "net/http/pprof"
)

// debugLogPath is the bounded file the diagnostic build mirrors slog into. The
// launcher spawns the service without capturing stderr, so phase timings from a
// local run are otherwise lost before they can be read.
func debugLogPath() string {
	return filepath.Join(desktopConfigDir(), "logs", "desktop-debug.log")
}

func debugLogWriter() io.Writer {
	path := debugLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return os.Stderr
	}
	if info, err := os.Stat(path); err == nil && info.Size() > 8<<20 {
		_ = os.Rename(path, path+".1")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, file)
}

// debugNote logs one diagnostic counter line with the tag enabled.
func debugNote(msg string, args ...any) {
	slog.Info("debugpprof: "+msg, args...)
}

// debugTickPhase times one maintenance phase. Windows CPU profiles attribute
// samples to threads blocked in syscalls, so the catalog tick is measured
// directly instead of by sampling.
func debugTickPhase(name string) func() {
	start := time.Now()
	return func() {
		slog.Info("debugpprof: tick phase", "phase", name, "took", time.Since(start).Round(time.Millisecond).String())
	}
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(debugLogWriter(), &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("debugpprof: log file", "path", debugLogPath())
	if os.Getenv("REASONIX_PPROF") == "1" {
		slog.Info("debugpprof: listening on 127.0.0.1:6060")
		go func() { _ = http.ListenAndServe("127.0.0.1:6060", nil) }()
	}
}
