package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/config"
)

const (
	fatalCrashDirName           = "crash-fatal"
	fatalCrashLogSuffix         = ".log"
	fatalCrashCoveredSuffix     = ".covered"
	legacyFatalCrashFile        = "crash-fatal.log"
	legacyFatalCrashCoveredFile = "crash-fatal-covered"
)

var fatalCrashProcessAlive = desktopProcessAlive

func fatalCrashDir() string {
	return filepath.Join(config.MemoryUserDir(), fatalCrashDirName)
}

func fatalCrashPath() string {
	return fatalCrashPathForPID(os.Getpid())
}

func fatalCrashCoveredPath() string {
	return fatalCrashCoveredPathForPID(os.Getpid())
}

func fatalCrashPathForPID(pid int) string {
	return filepath.Join(fatalCrashDir(), strconv.Itoa(pid)+fatalCrashLogSuffix)
}

func fatalCrashCoveredPathForPID(pid int) string {
	return filepath.Join(fatalCrashDir(), strconv.Itoa(pid)+fatalCrashCoveredSuffix)
}

func legacyFatalCrashPath() string {
	return filepath.Join(config.MemoryUserDir(), legacyFatalCrashFile)
}

func legacyFatalCrashCoveredPath() string {
	return filepath.Join(config.MemoryUserDir(), legacyFatalCrashCoveredFile)
}

func markFatalCrashCovered() {
	markFatalCrashCoveredForPID(os.Getpid())
}

func markFatalCrashCoveredForPID(pid int) {
	path := fatalCrashCoveredPathForPID(pid)
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = os.WriteFile(path, []byte("structured\n"), 0o600)
	}
}

// capturePreviousFatalCrash converts runtime.SetCrashOutput dumps from dead
// processes into the normal scrubbed queue. Per-PID files keep a routine second
// launch from truncating or unlinking the running primary process's dump.
func capturePreviousFatalCrash() {
	// Preserve compatibility with the single-file format used by older builds.
	// An empty legacy file may still be owned by a running older process, so it
	// must be left untouched until it contains a completed crash dump.
	captureFatalCrashFile(legacyFatalCrashPath(), legacyFatalCrashCoveredPath(), false)

	entries, err := os.ReadDir(fatalCrashDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		pid, ok := fatalCrashPID(entry.Name())
		if !ok || pid == os.Getpid() || fatalCrashProcessAlive(pid) {
			continue
		}
		captureFatalCrashFile(
			filepath.Join(fatalCrashDir(), entry.Name()),
			fatalCrashCoveredPathForPID(pid),
			true,
		)
	}
	for _, entry := range entries {
		pid, ok := fatalCrashCoveredPID(entry.Name())
		if !ok || pid == os.Getpid() || fatalCrashProcessAlive(pid) {
			continue
		}
		if _, err := os.Stat(fatalCrashPathForPID(pid)); os.IsNotExist(err) {
			_ = os.Remove(filepath.Join(fatalCrashDir(), entry.Name()))
		}
	}
	// Best effort: succeeds only when no live/current process artifacts remain.
	_ = os.Remove(fatalCrashDir())
}

func fatalCrashPID(name string) (int, bool) {
	return fatalCrashPIDWithSuffix(name, fatalCrashLogSuffix)
}

func fatalCrashCoveredPID(name string) (int, bool) {
	return fatalCrashPIDWithSuffix(name, fatalCrashCoveredSuffix)
}

func fatalCrashPIDWithSuffix(name, suffix string) (int, bool) {
	if !strings.HasSuffix(name, suffix) {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSuffix(name, suffix))
	return pid, err == nil && pid > 0
}

func captureFatalCrashFile(path, coveredPath string, removeEmpty bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	occurredAt := time.Now().UTC()
	if info, statErr := f.Stat(); statErr == nil {
		occurredAt = info.ModTime().UTC()
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, maxCrashStackBytes+1))
	_ = f.Close()
	if readErr != nil || len(strings.TrimSpace(string(raw))) == 0 {
		if removeEmpty {
			_ = os.Remove(coveredPath)
			_ = os.Remove(path)
		}
		return
	}
	if _, err := os.Stat(coveredPath); err == nil {
		_ = os.Remove(coveredPath)
		_ = os.Remove(path)
		return
	}
	stack := sanitizeFatalRuntimeDump(string(raw))
	report := baseCrashReport("crash")
	report.SchemaVersion = 2
	report.Source = "go.runtime"
	report.Label = "go.fatal"
	report.ErrorType = "GoRuntimeFatal"
	report.ErrorMessage = "Go runtime terminated the desktop process."
	report.Stack = stack
	report.TopFrame = topFrameFromStack(stack)
	report.FingerprintHint = "go.runtime.fatal"
	report.OccurredAt = occurredAt.Format(time.RFC3339)
	report.Message = sanitizeCrashText("[go.runtime.fatal]\n\n"+stack, maxCrashDetailBytes)
	if writePendingReport(report, true) {
		_ = os.Remove(coveredPath)
		_ = os.Remove(path)
	}
}

// sanitizeFatalRuntimeDump removes panic values and preamble text that could
// originate in user-controlled errors, while retaining runtime classification
// and symbolized goroutine stacks for diagnosis.
func sanitizeFatalRuntimeDump(raw string) string {
	lines := strings.Split(raw, "\n")
	classification := "runtime crash output"
	stackStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "fatal error:"):
			classification = sanitizeCrashText(trimmed, 256)
		case strings.HasPrefix(trimmed, "panic:"):
			classification = "panic: [redacted panic value]"
		}
		if strings.HasPrefix(trimmed, "goroutine ") {
			stackStart = i
			break
		}
	}
	stack := ""
	if stackStart >= 0 {
		stack = strings.Join(lines[stackStart:], "\n")
	}
	return sanitizeCrashText(classification+"\n\n"+stack, maxCrashStackBytes)
}

// installFatalCrashOutput asks the Go runtime to mirror unrecovered panics and
// fatal runtime errors to a durable file. The runtime duplicates the descriptor,
// so the file may be closed after SetCrashOutput returns.
func installFatalCrashOutput() {
	path := fatalCrashPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return
	}
	_ = f.Close()
}
