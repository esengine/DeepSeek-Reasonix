package crashreport

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const (
	fatalDirName    = "cli-crash-fatal"
	fatalSuffix     = ".log"
	maxFatalDumpLen = 64 << 10
)

// InstallFatalOutput mirrors what the Go runtime prints when it kills the
// process — a panic on any goroutine, a fatal runtime error — into a per-PID
// file under home, so the next start can queue it. Call release on every path
// that does not end in such a crash; it detaches the file and removes it.
func InstallFatalOutput(home string) (release func()) {
	release = func() {}
	if strings.TrimSpace(home) == "" {
		return release
	}
	path := filepath.Join(home, fatalDirName, strconv.Itoa(os.Getpid())+fatalSuffix)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return release
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return release
	}
	// The runtime duplicates the descriptor, so ours can close immediately.
	installErr := debug.SetCrashOutput(f, debug.CrashOptions{})
	_ = f.Close()
	if installErr != nil {
		_ = os.Remove(path)
		return release
	}
	return func() {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		_ = os.Remove(path)
	}
}

// CaptureFatalDumps queues the dumps left by processes that died after
// InstallFatalOutput, and discards the empty files of ones killed without one.
func CaptureFatalDumps(home, version string) {
	if strings.TrimSpace(home) == "" {
		return
	}
	dir := filepath.Join(home, fatalDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), fatalSuffix))
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), fatalSuffix) || err != nil ||
			pid <= 0 || pid == os.Getpid() || processAlive(pid) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if captureFatalDump(home, version, path) == nil {
			_ = os.Remove(path)
		}
	}
	_ = os.Remove(dir)
}

func captureFatalDump(home, version, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	occurredAt := time.Now().UTC()
	if info, statErr := f.Stat(); statErr == nil {
		occurredAt = info.ModTime().UTC()
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxFatalDumpLen))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil
	}
	classification, stack := splitFatalDump(string(raw))
	cleanStack := sanitizeStack(stack)
	return write(home, Report{
		Kind:          "crash",
		Version:       sanitizeField(defaultString(version, "unknown"), 64),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Message:       "[cli fatal]\n\nGo runtime terminated the CLI process.",
		SchemaVersion: currentSchemaVersion,
		ErrorType:     "GoRuntimeFatal",
		ErrorMessage:  classification,
		Stack:         cleanStack,
		TopFrame:      topFrame(cleanStack),
		OccurredAt:    occurredAt.Format(time.RFC3339Nano),
	})
}

// splitFatalDump keeps the runtime's own classification and the goroutine
// stacks. A panic value is user-influenced text and never leaves the dump.
func splitFatalDump(raw string) (classification, stack string) {
	const unclassified = "runtime crash output"
	classification = unclassified
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case classification != unclassified && !strings.HasPrefix(trimmed, "goroutine "):
			continue
		case strings.HasPrefix(trimmed, "fatal error:"):
			classification = sanitizeText(trimmed, 256)
		case strings.HasPrefix(trimmed, "panic:"):
			classification = "panic: [redacted panic value]"
		case strings.HasPrefix(trimmed, "goroutine "):
			return classification, strings.Join(lines[i:], "\n")
		}
	}
	return classification, ""
}
