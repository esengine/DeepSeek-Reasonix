package winsandbox

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The per-root lock serializes whole sandboxed runs (see the package comment in
// coordination_windows.go). An interactive command queued behind a long-running
// holder should fail fast with the holder named rather than hang its turn, so
// the default wait is short; a caller whose run nobody is blocked on (a
// background job) passes a longer budget via Spec.LockWait.
const defaultWindowsRootLockTimeout = time.Minute

// windowsRootLockTimeout resolves one run's lock wait budget: the explicit
// WINDOWS_SANDBOX_LOCK_MS override wins, then the caller's Spec.LockWait, then
// the short interactive default.
func windowsRootLockTimeout(specWait time.Duration) time.Duration {
	if raw := os.Getenv("WINDOWS_SANDBOX_LOCK_MS"); raw != "" {
		if ms, err := strconv.ParseUint(raw, 10, 63); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	if specWait > 0 {
		return specWait
	}
	return defaultWindowsRootLockTimeout
}

// A lock-holder record names the run currently holding one root lock so a
// queued command can tell the user what to stop. It is diagnostic only: written
// best-effort after the mutex is acquired, removed before it is released, and
// validated against a live PID on read, so a stale file left by a crash (the OS
// releases the abandoned mutex itself) is ignored and overwritten by the next
// holder of the same root. One line: "<pid>\t<start-unix-ms>\t<label>" — tabs
// separate the fields because the label is sanitized to never contain one.
type lockHolderRecord struct {
	pid         int
	startUnixMS int64
	label       string
}

func formatLockHolderRecord(rec lockHolderRecord) string {
	return strconv.Itoa(rec.pid) + "\t" + strconv.FormatInt(rec.startUnixMS, 10) + "\t" + rec.label + "\n"
}

func parseLockHolderRecord(data string) (lockHolderRecord, bool) {
	line, _, _ := strings.Cut(data, "\n")
	line = strings.TrimRight(line, "\r")
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return lockHolderRecord{}, false
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 0 {
		return lockHolderRecord{}, false
	}
	start, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || start <= 0 {
		return lockHolderRecord{}, false
	}
	return lockHolderRecord{pid: pid, startUnixMS: start, label: parts[2]}, true
}

// lockHolderLabelMaxRunes keeps the record one readable error-message line
// while leaving room for the shell's absolute path ahead of the user command.
const lockHolderLabelMaxRunes = 120

// lockHolderLabel renders argv as a one-line command preview for the holder
// record: control characters (including the record's tab separator) become
// spaces and long commands are truncated.
func lockHolderLabel(argv []string) string {
	label := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.Join(argv, " "))
	label = strings.TrimSpace(label)
	if r := []rune(label); len(r) > lockHolderLabelMaxRunes {
		return string(r[:lockHolderLabelMaxRunes]) + "…"
	}
	return label
}

// describeLockHolder renders a record for the queue notice / timeout error,
// e.g. `held by "npm run dev", pid 1234, running 25m`.
func describeLockHolder(rec lockHolderRecord, now time.Time) string {
	running := lockHolderRunningFor(now.Sub(time.UnixMilli(rec.startUnixMS)))
	if rec.label == "" {
		return fmt.Sprintf("held by pid %d, running %s", rec.pid, running)
	}
	return fmt.Sprintf("held by %q, pid %d, running %s", rec.label, rec.pid, running)
}

// lockHolderRunningFor formats an elapsed runtime at the precision a human
// needs to pick what to stop ("25m", not "25m3.021s"). Clock skew between the
// holder's and the waiter's reads clamps to 0s instead of going negative.
func lockHolderRunningFor(d time.Duration) string {
	switch {
	case d < time.Minute:
		if d < 0 {
			d = 0
		}
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// lockHolderFileName maps a root's mutex name (`Local\windows-sandbox.<hex>`)
// to its holder file, keyed by the same root digest so holder and mutex can
// never pair up differently.
func lockHolderFileName(mutexName string) string {
	digest := mutexName
	if i := strings.LastIndexByte(digest, '.'); i >= 0 {
		digest = digest[i+1:]
	}
	return digest + ".txt"
}
