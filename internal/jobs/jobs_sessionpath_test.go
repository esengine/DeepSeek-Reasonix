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

// TestValidateSessionPath exhaustively covers the transcript-path validator
// that guards SetActiveSessionPath against #6932 follow-up. Unlike
// validatePathSegment, this one permits `/` and `\` because sessionPath is an
// absolute or workspace-relative path; the boundary it enforces is that no
// path component is `..`.
func TestValidateSessionPath(t *testing.T) {
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
		// Path traversal — the actual attack.
		{"traversal with separators", "/safe/../escape.jsonl", true},
		{"traversal as full input", "../escape.jsonl", true},
		{"trailing dotdot", "/safe/dir/..", true},
		{"leading dotdot", "../etc/passwd.jsonl", true},
		// Windows-style backslash traversal. filepath.ToSlash normalizes them
		// before the split, so the same check catches `a\..\b`.
		{"windows backslash traversal", `C:\safe\..\etc\passwd.jsonl`, true},
		// Control characters and NUL.
		{"NUL byte", "/safe/abc\x00.jsonl", true},
		{"newline", "/safe/abc\n.jsonl", true},
		{"tab", "/safe/abc\t.jsonl", true},
		{"DEL char", "/safe/abc\x7f.jsonl", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSessionPath(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("validateSessionPath(%q) = nil, want error", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateSessionPath(%q) = %v, want nil", tc.input, err)
			}
		})
	}
}

// TestSetActiveSessionPath_RejectsTraversalPayload is the integration
// companion to TestValidateSessionPath. It proves that an adversarial
// sessionPath does not reach the filesystem or pollute the manager's
// artifactDirs cache. See #6932 follow-up.
func TestSetActiveSessionPath_RejectsTraversalPayload(t *testing.T) {
	root := t.TempDir()
	// Place a canary file in a path the payload would otherwise create or
	// write into. We construct the path with filepath.Join so the canary
	// lives in a deterministic location, but the payload we hand to
	// SetActiveSessionPath is built by string concatenation so the `..`
	// component survives to the validator (filepath.Join would collapse it).
	canaryDir := filepath.Join(root, "sibling")
	if err := os.MkdirAll(canaryDir, 0o700); err != nil {
		t.Fatalf("seed canary dir: %v", err)
	}
	canary := filepath.Join(canaryDir, "passwd.jsonl")
	if err := os.WriteFile(canary, []byte("pre-existing"), 0o600); err != nil {
		t.Fatalf("seed canary file: %v", err)
	}

	sink := &captureSink{}
	m := NewManager(sink)
	defer m.Close()

	// Raw concatenation preserves the `..` component. Without the validator
	// this would resolve to <root>/sibling/passwd.jsonl and SetActiveSessionPath
	// would create a sidecar at <root>/sibling/passwd.jobs, overwriting or
	// sitting next to the canary.
	sessionPath := root + string(os.PathSeparator) + "tmp" + string(os.PathSeparator) +
		".." + string(os.PathSeparator) + "sibling" + string(os.PathSeparator) + "passwd.jsonl"

	m.SetActiveSessionPath("session-a", sessionPath)

	// The canary must be intact: SetActiveSessionPath must not have created
	// a passwd.jobs sidecar directory next to it.
	got, err := os.ReadFile(canary)
	if err != nil {
		t.Fatalf("canary missing or unreadable: %v", err)
	}
	if string(got) != "pre-existing" {
		t.Fatalf("canary contents changed: %q", got)
	}
	canarySibling := filepath.Join(canaryDir, "passwd.jobs")
	if _, err := os.Stat(canarySibling); err == nil {
		t.Fatalf("artifact sidecar directory was created at %q; the fix failed", canarySibling)
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error for %q: %v", canarySibling, err)
	}

	// The manager must not have cached any directory for the rejected
	// session; a follow-up StartForSession should fall back to the temp
	// root, not to the rejected path.
	m.mu.Lock()
	cached, hasCached := m.artifactDirs["session-a"]
	m.mu.Unlock()
	if hasCached && cached != "" {
		t.Fatalf("artifactDirs cached %q for rejected sessionPath; expected no entry", cached)
	}
	if cached != "" {
		t.Fatalf("artifactDirs contains a non-empty entry for rejected sessionPath: %q", cached)
	}

	// The user-visible signal: a warning event hit the sink.
	if !sink.hasText("Ignoring SetActiveSessionPath with invalid session path") {
		t.Fatalf("expected warning emission, got events: %v", sink.texts())
	}
}

// TestSetActiveSessionPath_RejectsLeadingDotDot covers a second traversal
// shape from #6932 follow-up: a sessionPath that starts with `..` and so
// walks out of whatever the caller expected the artifact root to be.
// Built with raw concatenation so the `..` component is preserved for
// the validator to reject.
func TestSetActiveSessionPath_RejectsLeadingDotDot(t *testing.T) {
	sink := &captureSink{}
	m := NewManager(sink)
	defer m.Close()

	sessionPath := ".." + string(os.PathSeparator) + "etc" + string(os.PathSeparator) + "passwd.jsonl"

	m.SetActiveSessionPath("session-x", sessionPath)

	m.mu.Lock()
	_, hasCached := m.artifactDirs["session-x"]
	m.mu.Unlock()
	if hasCached {
		t.Fatal("artifactDirs cached an entry for a leading-dotdot sessionPath")
	}
	if !sink.hasText("Ignoring SetActiveSessionPath with invalid session path") {
		t.Fatalf("expected warning emission, got events: %v", sink.texts())
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
	if j.artifactErr != "" {
		t.Fatalf("artifactErr = %q, want empty", j.artifactErr)
	}
	if !strings.HasPrefix(j.artifactPath, want) {
		t.Fatalf("artifactPath = %q, want prefix %q", j.artifactPath, want)
	}
	<-done
	if !ran {
		t.Fatal("run callback never executed for the bound session")
	}
}
