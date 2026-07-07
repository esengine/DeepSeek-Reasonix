package winsandbox

import (
	"strings"
	"testing"
	"time"
)

func TestWindowsRootLockTimeoutPrecedence(t *testing.T) {
	// The explicit env override wins, then the caller's per-run budget, then the
	// short interactive default.
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "")
	if got := windowsRootLockTimeout(0); got != defaultWindowsRootLockTimeout {
		t.Fatalf("default = %s, want %s", got, defaultWindowsRootLockTimeout)
	}
	if got := windowsRootLockTimeout(10 * time.Minute); got != 10*time.Minute {
		t.Fatalf("spec budget = %s, want 10m", got)
	}
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "1234")
	if got := windowsRootLockTimeout(10 * time.Minute); got != 1234*time.Millisecond {
		t.Fatalf("env override = %s, want 1.234s", got)
	}
	// A malformed override falls back instead of hanging or failing.
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "not-a-number")
	if got := windowsRootLockTimeout(0); got != defaultWindowsRootLockTimeout {
		t.Fatalf("malformed env = %s, want %s", got, defaultWindowsRootLockTimeout)
	}
}

func TestLockHolderRecordRoundTrip(t *testing.T) {
	rec := lockHolderRecord{pid: 1234, startUnixMS: 1_700_000_000_000, label: `npm run dev`}
	got, ok := parseLockHolderRecord(formatLockHolderRecord(rec))
	if !ok || got != rec {
		t.Fatalf("round trip = %+v ok=%v, want %+v", got, ok, rec)
	}
	// A label containing spaces survives; tabs never occur (sanitized on write).
	rec.label = "cmd /c echo ok"
	if got, ok := parseLockHolderRecord(formatLockHolderRecord(rec)); !ok || got != rec {
		t.Fatalf("spaced label round trip = %+v ok=%v", got, ok)
	}
}

func TestLockHolderRecordRejectsMalformed(t *testing.T) {
	for _, data := range []string{
		"",
		"garbage",
		"12\t34",         // missing label field
		"0\t123\tcmd",    // pid 0 is never a real holder
		"-1\t123\tcmd",   // negative pid
		"nope\t123\tcmd", // non-numeric pid
		"12\tnope\tcmd",  // non-numeric start
		"12\t0\tcmd",     // zero start time
		"12 34 cmd",      // wrong separator
	} {
		if rec, ok := parseLockHolderRecord(data); ok {
			t.Fatalf("parse(%q) = %+v, want rejection", data, rec)
		}
	}
	// Only the first line counts; a trailing partial write cannot corrupt it.
	rec, ok := parseLockHolderRecord("12\t34\tcmd\ntrailing garbage")
	if !ok || rec.label != "cmd" {
		t.Fatalf("multi-line parse = %+v ok=%v, want first line only", rec, ok)
	}
}

func TestLockHolderLabelSanitizesAndTruncates(t *testing.T) {
	got := lockHolderLabel([]string{"pwsh", "-Command", "npm\trun\ndev"})
	if strings.ContainsAny(got, "\t\n\r") {
		t.Fatalf("label %q still contains control characters", got)
	}
	if got != "pwsh -Command npm run dev" {
		t.Fatalf("label = %q", got)
	}
	long := lockHolderLabel([]string{strings.Repeat("x", 500)})
	if r := []rune(long); len(r) > lockHolderLabelMaxRunes+1 {
		t.Fatalf("label not truncated: %d runes", len(r))
	}
	if !strings.HasSuffix(long, "…") {
		t.Fatalf("truncated label %q missing ellipsis", long)
	}
}

func TestDescribeLockHolder(t *testing.T) {
	start := time.UnixMilli(1_700_000_000_000)
	rec := lockHolderRecord{pid: 1234, startUnixMS: start.UnixMilli(), label: "npm run dev"}
	if got := describeLockHolder(rec, start.Add(25*time.Minute)); got != `held by "npm run dev", pid 1234, running 25m` {
		t.Fatalf("describe = %q", got)
	}
	rec.label = ""
	if got := describeLockHolder(rec, start.Add(3*time.Second)); got != "held by pid 1234, running 3s" {
		t.Fatalf("describe without label = %q", got)
	}
}

func TestLockHolderRunningFor(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-5 * time.Second, "0s"}, // clock skew clamps, never negative
		{12 * time.Second, "12s"},
		{90 * time.Second, "1m"},
		{25 * time.Minute, "25m"},
		{2*time.Hour + 5*time.Minute, "2h05m"},
	} {
		if got := lockHolderRunningFor(tc.d); got != tc.want {
			t.Fatalf("runningFor(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestLockHolderFileNameMatchesMutexDigest(t *testing.T) {
	// The holder file is keyed by the same root digest as the mutex name, so the
	// two can never pair up differently.
	if got := lockHolderFileName(`Local\windows-sandbox.deadbeef00112233`); got != "deadbeef00112233.txt" {
		t.Fatalf("holder file = %q, want digest-keyed name", got)
	}
}
