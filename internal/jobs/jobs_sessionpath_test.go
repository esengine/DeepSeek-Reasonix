package jobs

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/store"
)

// captureSink records every event the manager emits so tests can assert that
// the validation-failure path emitted the expected warning.
type captureSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *captureSink) Emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *captureSink) texts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.events))
	for _, ev := range s.events {
		if ev.Text != "" {
			out = append(out, ev.Text)
		}
	}
	return out
}

func (s *captureSink) hasText(needle string) bool {
	for _, t := range s.texts() {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

// TestValidateTrustedSessionPath covers the defense-in-depth syntax validator
// for transcript paths supplied by the trusted store/controller layer. It is
// deliberately not a trusted-root containment check.
func TestValidateTrustedSessionPath(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Empty is rejected (caller must provide a transcript path).
		{"empty is rejected", "", true},
		// Absolute and relative transcript paths in normal use.
		{"absolute posix path", "/home/u/.reasonix/sessions/abc.jsonl", false},
		{"absolute windows path", `C:\Users\me\.reasonix\sessions\abc.jsonl`, false},
		{"workspace-relative", "sessions/abc.jsonl", false},
		// Filenames with a hidden segment are still legitimate.
		{"..hidden is allowed", "/home/u/.reasonix/..hidden.jsonl", false},
		{"triple-dot is allowed", "/home/u/...jsonl", false},
		// Trusted paths keep their host-path semantics. This validator is not a
		// trusted-root containment boundary.
		{"dotdot with separators", "/safe/../session.jsonl", false},
		{"leading dotdot", "../sessions/abc.jsonl", false},
		{"trailing dotdot", "/safe/dir/..", false},
		{"windows backslashes", `C:\safe\..\sessions\abc.jsonl`, false},
		// Control characters and NUL.
		{"NUL byte", "/safe/abc\x00.jsonl", true},
		{"newline", "/safe/abc\n.jsonl", true},
		{"tab", "/safe/abc\t.jsonl", true},
		{"DEL char", "/safe/abc\x7f.jsonl", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTrustedSessionPath(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("validateTrustedSessionPath(%q) = nil, want error", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateTrustedSessionPath(%q) = %v, want nil", tc.input, err)
			}
		})
	}
}

// TestSetActiveSessionPath_AcceptsTrustedDotDotPath preserves valid relative
// transcript spellings used by callers such as headless --resume.
func TestSetActiveSessionPath_AcceptsTrustedDotDotPath(t *testing.T) {
	root := t.TempDir()
	intermediate := filepath.Join(root, "intermediate")
	if err := os.MkdirAll(intermediate, 0o700); err != nil {
		t.Fatalf("create intermediate dir: %v", err)
	}
	sink := &captureSink{}
	m := NewManager(sink)
	defer m.Close()

	// Raw concatenation preserves the trusted `..` spelling that filepath.Join
	// would otherwise clean before it reaches SetActiveSessionPath.
	sessionPath := intermediate + string(os.PathSeparator) + ".." +
		string(os.PathSeparator) + "session.jsonl"

	m.SetActiveSessionPath("session-a", sessionPath)

	m.mu.Lock()
	active := m.active
	cached := m.artifactDirs["session-a"]
	m.mu.Unlock()
	if active != "session-a" {
		t.Fatalf("active = %q, want %q", active, "session-a")
	}
	wantDir := store.SessionJobsDir(sessionPath)
	if cached != wantDir {
		t.Fatalf("cached dir = %q, want %q", cached, wantDir)
	}
	if sink.hasText("Ignoring SetActiveSessionPath with invalid session path") {
		t.Fatalf("trusted dotdot path unexpectedly emitted a warning: %v", sink.texts())
	}

	j := m.StartForSession("session-a", "bash", "trusted relative path", func(_ context.Context, _ io.Writer) (string, error) {
		return "ok", nil
	})
	<-j.done
	if j.artifactErr != "" {
		t.Fatalf("artifactErr = %q, want empty", j.artifactErr)
	}
	if got, want := filepath.Clean(filepath.Dir(j.artifactPath)), filepath.Join(root, "session.jobs"); got != want {
		t.Fatalf("artifact dir = %q, want %q", got, want)
	}
}

// TestSetActiveSessionPath_InvalidPathUpdatesActiveAndClearsBinding verifies
// that filesystem rejection does not leave lifecycle notices or future jobs
// attached to the previous session/path.
func TestSetActiveSessionPath_InvalidPathUpdatesActiveAndClearsBinding(t *testing.T) {
	sink := &captureSink{}
	m := NewManager(sink)
	defer m.Close()

	m.SetActiveSessionPath("session-x", filepath.Join(t.TempDir(), "old.jsonl"))
	m.SetActiveSession("old-session")
	m.SetActiveSessionPath("session-x", filepath.Join(t.TempDir(), "bad\npath.jsonl"))

	m.mu.Lock()
	active := m.active
	_, hasCached := m.artifactDirs["session-x"]
	_, loaded := m.loaded["session-x"]
	m.mu.Unlock()
	if active != "session-x" {
		t.Fatalf("active = %q, want %q", active, "session-x")
	}
	if hasCached {
		t.Fatal("artifactDirs retained the stale binding for a rejected sessionPath")
	}
	if loaded {
		t.Fatal("loaded retained the stale binding for a rejected sessionPath")
	}
	if !sink.hasText("Ignoring SetActiveSessionPath with invalid session path") {
		t.Fatalf("expected warning emission, got events: %v", sink.texts())
	}
}

// TestSetActiveSessionPath_EmptyPathUpdatesActiveOnly preserves the legacy
// active-session update used before a persistent transcript path is available.
func TestSetActiveSessionPath_EmptyPathUpdatesActiveOnly(t *testing.T) {
	sink := &captureSink{}
	m := NewManager(sink)
	defer m.Close()

	m.SetActiveSessionPath("session-active", "")

	m.mu.Lock()
	active := m.active
	_, hasCached := m.artifactDirs["session-active"]
	m.mu.Unlock()
	if active != "session-active" {
		t.Fatalf("active = %q, want %q", active, "session-active")
	}
	if hasCached {
		t.Fatal("empty sessionPath unexpectedly populated artifactDirs")
	}
	if sink.hasText("Ignoring SetActiveSessionPath with invalid session path") {
		t.Fatalf("empty sessionPath unexpectedly emitted a warning: %v", sink.texts())
	}
}

// TestSetActiveSessionPath_AcceptsValidInput is a regression guard: the
// validator must not break legitimate callers that pass typical transcript
// paths produced by store.SessionTranscriptPath or filepath.Join(t.TempDir(), "...").
func TestSetActiveSessionPath_AcceptsValidInput(t *testing.T) {
	sink := &captureSink{}
	m := NewManager(sink)
	defer m.Close()

	sessionPath := filepath.Join(t.TempDir(), "abc.jsonl")
	m.SetActiveSessionPath("session-a", sessionPath)

	m.mu.Lock()
	cached, hasCached := m.artifactDirs["session-a"]
	m.mu.Unlock()
	if !hasCached || cached == "" {
		t.Fatal("artifactDirs missing the entry for the accepted sessionPath")
	}
	want := store.SessionJobsDir(sessionPath)
	if cached != want {
		t.Fatalf("cached dir = %q, want %q", cached, want)
	}
	if sink.hasText("Ignoring SetActiveSessionPath with invalid session path") {
		t.Fatalf("unexpected warning emission for valid input: %v", sink.texts())
	}

	// Follow-up StartForSession in the bound session produces an artifact
	// next to the transcript, demonstrating the cache path is intact.
	ran := false
	done := make(chan struct{})
	j := m.StartForSession("session-a", "bash", "round trip", func(_ context.Context, _ io.Writer) (string, error) {
		ran = true
		close(done)
		return "ok", nil
	})
	j.mu.Lock()
	artifactErr := j.artifactErr
	artifactPath := j.artifactPath
	j.mu.Unlock()
	if artifactErr != "" {
		t.Fatalf("artifactErr = %q, want empty", artifactErr)
	}
	if !strings.HasPrefix(artifactPath, want) {
		t.Fatalf("artifactPath = %q, want prefix %q", artifactPath, want)
	}
	<-done
	if !ran {
		t.Fatal("run callback never executed for the bound session")
	}
}
