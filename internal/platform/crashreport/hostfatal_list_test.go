package crashreport

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestListFatalLogsNewestFirstAndSkipsWhatIsNotACrashFile(t *testing.T) {
	dir := testenv.TempDir(t)
	write := func(name, body string, age time.Duration) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		_ = os.Chtimes(path, at, at)
	}
	write("host-20261010T070806Z-2.31.0-100"+fatalSuffix, "fatal error: out of memory", 2*time.Hour)
	write("host-20261011T070806Z-2.33.0-200"+fatalSuffix, "panic", time.Hour)
	write("host-empty"+fatalSuffix, "", time.Minute)
	write("notes.txt", "x", time.Minute)
	got, truncated := ListFatalLogs(dir)
	if truncated || len(got) != 2 || got[0].Name != "host-20261011T070806Z-2.33.0-200"+fatalSuffix || got[1].Bytes != int64(len("fatal error: out of memory")) {
		t.Fatalf("list = %+v", got)
	}
	if none, cut := ListFatalLogs(filepath.Join(dir, "absent")); none != nil || cut {
		t.Fatal("a missing directory lists something")
	}
}

func TestListFatalLogsIsBounded(t *testing.T) {
	dir := testenv.TempDir(t)
	for i := range MaxListedFatalLogs + 7 {
		name := filepath.Join(dir, fmt.Sprintf("host-%03d", i)+fatalSuffix)
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-time.Duration(i) * time.Minute)
		_ = os.Chtimes(name, at, at)
	}
	got, truncated := ListFatalLogs(dir)
	if len(got) != MaxListedFatalLogs || !truncated || got[0].Name != "host-000"+fatalSuffix {
		t.Fatalf("listed %d truncated=%v first=%q", len(got), truncated, got[0].Name)
	}
}
