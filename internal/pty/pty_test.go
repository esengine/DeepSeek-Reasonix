package pty

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(16)

	// Test basic write and read
	_, _ = rb.Write([]byte("hello"))
	if got := string(rb.ReadUnread(100)); got != "hello" {
		t.Fatalf("ReadUnread = %q, want %q", got, "hello")
	}

	// Reading unread again should be empty
	if got := string(rb.ReadUnread(100)); got != "" {
		t.Fatalf("ReadUnread after empty = %q, want empty", got)
	}

	// Test buffer overflow wrapping
	_, _ = rb.Write([]byte("1234567890abcdefghij")) // 20 bytes into 16-byte buffer
	all := string(rb.Bytes())
	if len(all) != 16 {
		t.Fatalf("len(Bytes) = %d, want 16", len(all))
	}
	if all != "567890abcdefghij" {
		t.Fatalf("Bytes() = %q, want %q", all, "567890abcdefghij")
	}

	// Read tail
	tail := string(rb.ReadTail(4))
	if tail != "ghij" {
		t.Fatalf("ReadTail = %q, want %q", tail, "ghij")
	}

	// Reset
	rb.Reset()
	if rb.Len() != 0 {
		t.Fatalf("Len() after reset = %d, want 0", rb.Len())
	}
}

func TestCleanTerminalOutput(t *testing.T) {
	// ANSI Color codes
	colored := "\x1b[31mRed Text\x1b[0m \x1b[32mGreen\x1b[0m"
	if got := CleanTerminalOutput(colored); got != "Red Text Green" {
		t.Fatalf("CleanTerminalOutput(colored) = %q, want %q", got, "Red Text Green")
	}

	// Standalone Carriage Returns (simulating progress bar / overwrite)
	progress := "Downloading: 10%\rDownloading: 50%\rDownloading: 100%\nDone!"
	expected := "Downloading: 100%\nDone!"
	if got := CleanTerminalOutput(progress); got != expected {
		t.Fatalf("CleanTerminalOutput(progress) = %q, want %q", got, expected)
	}

	// Windows CRLF
	crlf := "line 1\r\nline 2\r\n"
	if got := CleanTerminalOutput(crlf); got != "line 1\nline 2\n" {
		t.Fatalf("CleanTerminalOutput(crlf) = %q, want %q", got, "line 1\nline 2\n")
	}
}

func TestPTYManagerLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "reasonix-pty-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	defer mgr.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Start a PTY session
	sess, err := mgr.Start(ctx, StartOptions{
		ID:  "test-session",
		Cwd: tmpDir,
	})
	if err != nil {
		t.Fatalf("mgr.Start failed: %v", err)
	}
	if !sess.IsRunning() {
		t.Fatalf("expected session to be running")
	}

	// 2. Initial prompt settle wait
	initialOut, err := sess.Write(ctx, "", 300*time.Millisecond)
	if err != nil {
		t.Logf("initial settle error: %v", err)
	}
	t.Logf("Initial prompt: %q", initialOut)

	// 3. Command 1: Export and Echo variable
	out1, err := mgr.Write(ctx, "test-session", "export REASONIX_TEST_VAR=42\necho VAR_IS_$REASONIX_TEST_VAR\n", 600*time.Millisecond)
	if err != nil {
		t.Fatalf("mgr.Write failed: %v", err)
	}
	if !strings.Contains(out1, "VAR_IS_42") {
		// Try reading unread if prompt was slow
		time.Sleep(200 * time.Millisecond)
		out1 += mgr.sessions["test-session"].Read(1024)
		if !strings.Contains(out1, "VAR_IS_42") {
			t.Fatalf("output does not contain VAR_IS_42: %q", out1)
		}
	}

	// 4. Command 2: Verify state persisted across turns (echo again in separate turn)
	out2, err := mgr.Write(ctx, "test-session", "echo STILL_$REASONIX_TEST_VAR\n", 600*time.Millisecond)
	if err != nil {
		t.Fatalf("mgr.Write 2 failed: %v", err)
	}
	if !strings.Contains(out2, "STILL_42") {
		time.Sleep(200 * time.Millisecond)
		out2 += mgr.sessions["test-session"].Read(1024)
		if !strings.Contains(out2, "STILL_42") {
			t.Fatalf("output 2 does not contain STILL_42: %q", out2)
		}
	}

	// 5. Test List
	list := mgr.List()
	if len(list) != 1 || list[0].ID != "test-session" {
		t.Fatalf("List() returned %+v, want 1 session with id 'test-session'", list)
	}

	// 6. Test Close
	if err := mgr.Close("test-session"); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if len(mgr.List()) != 0 {
		t.Fatalf("List() after close = %d, want 0", len(mgr.List()))
	}
}

func TestMarkerLinePresentDiscrimination(t *testing.T) {
	marker := markerPrefix + "testtag123__"
	driver := &PosixShellDriver{}

	// 1. Terminal input echo of the command itself must NOT match
	cmdEcho := "printf '\\n__REASONIX_DONE_testtag123__:%s\\n' \"$?\""
	if _, ok := driver.ParseSentinel(cmdEcho, marker); ok {
		t.Fatalf("command echo was mistaken for a valid sentinel line: %q", cmdEcho)
	}

	// 2. Fragmentary or partial matches must NOT match
	partials := []string{
		"__REASONIX_DONE_testtag123__",
		"__REASONIX_DONE_testtag123__:",
		"__REASONIX_DONE_testtag123__:abc",
		"echo __REASONIX_DONE_testtag123__:0",
	}
	for _, p := range partials {
		if _, ok := driver.ParseSentinel(p, marker); ok {
			t.Fatalf("partial/invalid line %q matched sentinel pattern", p)
		}
	}

	// 3. Genuine sentinel line MUST match and extract exit code
	valids := map[string]int{
		"__REASONIX_DONE_testtag123__:0":      0,
		"__REASONIX_DONE_testtag123__:1":      1,
		"__REASONIX_DONE_testtag123__:127":    127,
		"  __REASONIX_DONE_testtag123__:0\r\n": 0,
	}
	for v, wantCode := range valids {
		code, ok := driver.ParseSentinel(v, marker)
		if !ok {
			t.Fatalf("valid sentinel line %q was not recognized", v)
		}
		if code != wantCode {
			t.Fatalf("ParseSentinel(%q) = %d, want %d", v, code, wantCode)
		}
	}

	// 4. CompletionParser cleans out sentinel and cmdEcho matching this specific marker
	cp := NewCompletionParser(nil)
	notifyCh := cp.Arm("req-1", marker, driver)
	combinedOutput := "line 1\nline 2\n" + cmdEcho + "\n__REASONIX_DONE_testtag123__:0\n"
	cp.Feed([]byte(combinedOutput))

	select {
	case <-notifyCh:
	default:
		t.Fatalf("expected notifyCh to fire on sentinel match")
	}

	out, code, matched := cp.Result()
	if !matched || code != 0 {
		t.Fatalf("expected matched=true, code=0, got matched=%v, code=%d", matched, code)
	}
	if out != "line 1\nline 2" {
		t.Fatalf("CompletionParser result output = %q, want 'line 1\\nline 2'", out)
	}

	// 5. User grep output containing __REASONIX_DONE_ (with different text) must NOT be truncated
	cp2 := NewCompletionParser(nil)
	_ = cp2.Arm("req-2", marker, driver)
	grepOutput := "line 1\npty.go: const markerPrefix = \"__REASONIX_DONE_\"\n" + cmdEcho + "\n__REASONIX_DONE_testtag123__:42\n"
	cp2.Feed([]byte(grepOutput))
	out2, code2, matched2 := cp2.Result()
	if !matched2 || code2 != 42 {
		t.Fatalf("expected matched=true, code=42, got matched=%v, code=%d", matched2, code2)
	}
	if out2 != "line 1\npty.go: const markerPrefix = \"__REASONIX_DONE_\"" {
		t.Fatalf("user grep output was incorrectly truncated: %q", out2)
	}
}

