package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

// seedNativeSessionMessages publishes one canonical v4 session with one user
// turn per prompt, so its catalog metadata is durable and it is visible to the
// resume surface. The writer is closed before returning so a later host can
// attach to it. A non-empty modelRef records the saved model selection.
func seedNativeSessionMessages(t *testing.T, dir, id, modelRef, modelIdentity string, prompts ...string) {
	t.Helper()
	service, err := session.NewService("local", session.NewFilesystemPersistence(session.RootForLegacyDir(dir)))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	runtime, err := service.Create(context.Background(), session.CreateOptions{SessionID: id})
	if err != nil {
		_ = service.CloseAll(context.Background())
		t.Fatalf("create native session: %v", err)
	}
	for i, prompt := range prompts {
		turnID := fmt.Sprintf("turn-%s-%d", id, i)
		events := []session.Event{{Kind: "turn/start"}}
		if i == 0 && modelRef != "" {
			cfgPayload, err := json.Marshal(map[string]string{"modelRef": modelRef, "modelIdentity": modelIdentity})
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}
			events = append(events, session.Event{Kind: "session/config", Payload: cfgPayload})
		}
		payload, err := json.Marshal(map[string]any{"message": provider.Message{
			ID: fmt.Sprintf("m-%s-%d", id, i), Role: provider.RoleUser, Content: prompt,
		}})
		if err != nil {
			t.Fatalf("marshal message: %v", err)
		}
		events = append(events,
			session.Event{Kind: "message/complete", Payload: payload},
			session.Event{Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)},
		)
		if _, err := runtime.Session().Append(context.Background(), session.Batch{
			OperationID: fmt.Sprintf("op-%s-%d", id, i), TurnID: turnID, Events: events,
		}); err != nil {
			t.Fatalf("append native turn: %v", err)
		}
	}
	if _, err := runtime.Session().Flush(context.Background()); err != nil {
		t.Fatalf("flush native session: %v", err)
	}
	if err := service.CloseAll(context.Background()); err != nil {
		t.Fatalf("close seeding service: %v", err)
	}
}

// seedNativeSession publishes a native v4 session with a single user turn.
func seedNativeSession(t *testing.T, dir, id, prompt string) {
	t.Helper()
	seedNativeSessionMessages(t, dir, id, "", "", prompt)
}

// newExclusiveTestController builds an identity-bound controller over dir, so
// the v4 resume surface (OpenSession) is exercised as in production.
func newExclusiveTestController(t *testing.T, dir string, exec *agent.Agent) *control.Controller {
	t.Helper()
	service, err := session.NewService("local", session.NewFilesystemPersistence(session.RootForLegacyDir(dir)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	return newOwnedTestController(t, control.Options{
		Executor: exec, SessionDir: dir, Label: "test",
		SessionService: service, ExclusiveSession: true,
	})
}

// seedNativeSessionWithModel records a session/config event so the resume
// surface can restore the saved model selection.
func seedNativeSessionWithModel(t *testing.T, dir, id, prompt, modelRef, modelIdentity string) {
	t.Helper()
	seedNativeSessionMessages(t, dir, id, modelRef, modelIdentity, prompt)
}

// saveTestSession writes a legacy JSONL transcript for tests that still
// exercise the compatibility readers and the one-time startup migration.
func saveTestSession(t *testing.T, path, prompt string) {
	t.Helper()
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: prompt})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
}

var nativeTestSeq uint64

// nextNativeTestID mints a canonical 32-hex identity for a seeded v4 session.
func nextNativeTestID() string {
	return fmt.Sprintf("%032x", atomic.AddUint64(&nativeTestSeq, 1))
}

// seedNativeTestSession publishes a native v4 session and returns its id.
func seedNativeTestSession(t *testing.T, dir, prompt string) string {
	t.Helper()
	id := nextNativeTestID()
	seedNativeSession(t, dir, id, prompt)
	return id
}

func TestV4ResumeLocatorRoundTrip(t *testing.T) {
	if _, ok := v4ResumeID("/tmp/legacy.jsonl"); ok {
		t.Fatal("a legacy path must not decode as a v4 locator")
	}
	locator := v4ResumeLocator("abc123")
	if !isNativeResume(locator) {
		t.Fatalf("locator %q not recognized as native", locator)
	}
	id, ok := v4ResumeID(locator)
	if !ok || id != "abc123" {
		t.Fatalf("v4ResumeID(%q) = %q, %v", locator, id, ok)
	}
}

func TestIsCanonicalSessionID(t *testing.T) {
	for _, id := range []string{
		"21c700d4f5de2796c1a1f47455cc3917", // 32-hex random id
		"3d925165b6b16f257ae10db0",         // 24-hex migration id
	} {
		if !isCanonicalSessionID(id) {
			t.Fatalf("%q should be canonical", id)
		}
	}
	for _, id := range []string{"a-active", "20260914-110358.140695220", "ABCDEF", ""} {
		if isCanonicalSessionID(id) {
			t.Fatalf("%q should not be canonical", id)
		}
	}
}

func TestResumeRowsIncludeNativeSession(t *testing.T) {
	dir := t.TempDir()
	saveTestSession(t, filepath.Join(dir, "legacy.jsonl"), "legacy prompt")
	const nativeID = "21c700d4f5de2796c1a1f47455cc3917"
	seedNativeSession(t, dir, nativeID, "native prompt")

	rows := resumeRows(dir)
	if len(rows) != 1 {
		t.Fatalf("resume rows = %+v, want only the native v4 session", rows)
	}
	id, ok := v4ResumeID(rows[0].Path)
	if !ok || id != nativeID {
		t.Fatalf("row = %q, want native locator for %q", rows[0].Path, nativeID)
	}
	if rows[0].Preview == "" {
		t.Fatalf("native row has no preview: %+v", rows[0])
	}
}

func TestResumeRowsHideManagedMirror(t *testing.T) {
	dir := t.TempDir()
	// A managed session-events mirror is keyed by the legacy branch id and is
	// not an independent native conversation.
	service, err := session.NewService("local", session.NewFilesystemPersistence(session.RootForLegacyDir(dir)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	if _, err := service.Create(context.Background(), session.CreateOptions{SessionID: "a-active"}); err != nil {
		t.Fatalf("create mirror: %v", err)
	}
	saveTestSession(t, filepath.Join(dir, "a-active.jsonl"), "legacy active")

	for _, row := range resumeRows(dir) {
		if id, ok := v4ResumeID(row.Path); ok {
			t.Fatalf("managed mirror %q leaked into resume rows", id)
		}
	}
}

func TestResumeRowsDedupMigratedLegacy(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "old.jsonl")
	saveTestSession(t, legacyPath, "migrated prompt")
	if _, err := session.MigrateLegacy(context.Background(), legacyPath, session.RootForLegacyDir(dir)); err != nil {
		t.Fatalf("migrate legacy: %v", err)
	}

	rows := resumeRows(dir)
	if len(rows) != 1 {
		t.Fatalf("resume rows = %+v, want exactly the migrated v4 session", rows)
	}
	if _, ok := v4ResumeID(rows[0].Path); !ok {
		t.Fatalf("expected the v4 session to replace the migrated legacy row, got %q", rows[0].Path)
	}
}

func TestMostRecentSessionPrefersNativeV4(t *testing.T) {
	dir := t.TempDir()
	saveTestSession(t, filepath.Join(dir, "legacy.jsonl"), "legacy prompt")
	const nativeID = "7c1984175ae4c607fe1b58ffaef0ed72"
	seedNativeSession(t, dir, nativeID, "native prompt")

	latest, ok := mostRecentSession(dir)
	if !ok {
		t.Fatal("mostRecentSession found nothing")
	}
	if id, native := v4ResumeID(latest.Path); !native || id != nativeID {
		t.Fatalf("most recent = %q, want native v4 session %q", latest.Path, nativeID)
	}
}

// TestMigratedSessionKeepsSourceActivityTime proves a migrated legacy session
// is ordered by when the conversation happened, not by when it was imported, so
// --continue can still reach a genuinely newer native session.
func TestMigratedSessionKeepsSourceActivityTime(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "old.jsonl")
	saveTestSession(t, legacyPath, "migrated prompt")
	updated := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	meta := fmt.Sprintf(`{"id":"old","created_at":%q,"updated_at":%q}`,
		updated.Add(-time.Hour).UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(legacyPath+".meta", []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.MigrateLegacy(context.Background(), legacyPath, session.RootForLegacyDir(dir)); err != nil {
		t.Fatalf("migrate legacy: %v", err)
	}

	rows := resumeRows(dir)
	if len(rows) != 1 {
		t.Fatalf("resume rows = %+v, want exactly the migrated session", rows)
	}
	if !rows[0].LastActivityAt.Equal(updated) {
		t.Fatalf("migrated row activity = %v, want the source activity %v", rows[0].LastActivityAt, updated)
	}
}

func TestResolveSessionQueryMatchesNativeIDAndTitle(t *testing.T) {
	dir := t.TempDir()
	const nativeID = "5b4f3ab2887ced4c6b181ab9dc7a990e"
	seedNativeSession(t, dir, nativeID, "unique native prompt")

	got, err := resolveSessionQuery(dir, nativeID)
	if err != nil {
		t.Fatalf("resolve by id: %v", err)
	}
	if id, ok := v4ResumeID(got); !ok || id != nativeID {
		t.Fatalf("resolve by id = %q, want native locator for %q", got, nativeID)
	}

	got, err = resolveSessionQuery(dir, "unique native")
	if err != nil {
		t.Fatalf("resolve by preview: %v", err)
	}
	if id, ok := v4ResumeID(got); !ok || id != nativeID {
		t.Fatalf("resolve by preview = %q, want native locator for %q", got, nativeID)
	}
}

func TestResumeWithPersistedSelectionOpensNativeSession(t *testing.T) {
	dir := t.TempDir()
	const nativeID = "21c700d4f5de2796c1a1f47455cc3917"
	seedNativeSession(t, dir, nativeID, "native prompt")

	service, err := session.NewService("local", session.NewFilesystemPersistence(session.RootForLegacyDir(dir)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := newOwnedTestController(t, control.Options{
		Executor: exec, SessionDir: dir, Label: "test",
		SessionService: service, ExclusiveSession: true,
	})

	if err := resumeWithPersistedSelection(ctrl, v4ResumeLocator(nativeID)); err != nil {
		t.Fatalf("resume native v4 session: %v", err)
	}
	ref, ok := ctrl.SessionRef()
	if !ok || ref.SessionID != nativeID {
		t.Fatalf("controller ref = %+v ok=%v, want session %q", ref, ok, nativeID)
	}
	if got := ctrl.History(); len(got) == 0 {
		t.Fatal("resumed native session has empty history")
	}
}
