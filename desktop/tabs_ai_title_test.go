package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/config"
)

// aiTitleFixture wires an isolated user config with ai_session_title on and a
// fake OpenAI-compatible provider returning the given title text (or an error
// chunk when fail is set). It returns the app, the tab, the session path, and
// a request counter so tests can assert how many title requests were made.
func aiTitleFixture(t *testing.T, responseText string, fail bool) (*App, *WorkspaceTab, string, *atomic.Int32) {
	t.Helper()
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "TEST_MODEL_KEY", "sk-test")

	var requests atomic.Int32
	providerStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if fail {
			_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"boom\"}}\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":"+strconv.Quote(responseText)+"}}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(providerStub.Close)

	cfg := config.Default()
	cfg.DefaultModel = "test/test-model"
	cfg.Desktop.AISessionTitle = true
	cfg.Providers = []config.ProviderEntry{
		{Name: "test", Kind: "openai", BaseURL: providerStub.URL, Model: "test-model", APIKeyEnv: "TEST_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	root := t.TempDir()
	sessionDir := filepath.Join(root, ".reasonix", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("make session dir: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, sessionDir, "session-a.jsonl", "topic_ai_1", "topic title", root, "fix the login loop", time.Now())

	tab := &WorkspaceTab{
		ID:            "t1",
		Scope:         "project",
		WorkspaceRoot: root,
		TopicID:       "topic_ai_1",
		TopicTitle:    "topic title",
		Ready:         true,
		model:         "test/test-model",
		disabledMCP:   map[string]ServerView{},
	}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}, tabOrder: []string{tab.ID}, activeTabID: tab.ID}
	installNoopRuntimeEvents(app)
	// A freshly created topic already carries an auto-sourced title entry, as
	// the real new-session path writes before the first snapshot.
	if err := setTopicTitleWithSource(root, "topic_ai_1", "topic title", topicTitleSourceAuto); err != nil {
		t.Fatalf("seed topic title: %v", err)
	}
	return app, tab, sessionPath, &requests
}

// waitForAITitle polls the topic title file until the async request has
// finished writing, or fails the test after the deadline.
func waitForAITitle(t *testing.T, app *App, tab *WorkspaceTab, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := loadTopicTitle(tab.WorkspaceRoot, tab.TopicID); got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("topic title = %q, want %q", loadTopicTitle(tab.WorkspaceRoot, tab.TopicID), want)
}

func TestMaybeGenerateAISessionTitleWithBasisSkipsDisk(t *testing.T) {
	app, tab, _, requests := aiTitleFixture(t, "Debug login loop", false)
	// The submit-time path passes the message directly; no session file read
	// is required, so even a missing session path must not block generation.
	app.maybeGenerateAISessionTitleWithBasis(context.Background(), tab, tab.WorkspaceRoot, tab.TopicID, "", "fix the login loop", aiTitleSnapshotBudget)
	waitForAITitle(t, app, tab, "Debug login loop")
	if requests.Load() != 1 {
		t.Fatalf("title requests = %d, want 1", requests.Load())
	}
}

func TestAISessionTitleBasisHashStable(t *testing.T) {
	h1 := aiTitleBasisHash("fix the login loop")
	h2 := aiTitleBasisHash("fix the login loop")
	if h1 != h2 {
		t.Fatalf("same basis produced different hashes: %q vs %q", h1, h2)
	}
	if h1 == aiTitleBasisHash("fix the logout loop") {
		t.Fatalf("different basis produced the same hash %q", h1)
	}
}

func TestGenerateAISessionTitleWritesSummary(t *testing.T) {
	app, tab, sessionPath, requests := aiTitleFixture(t, "Debug login loop", false)
	app.maybeGenerateAISessionTitle(tab, tab.WorkspaceRoot, tab.TopicID, sessionPath)

	waitForAITitle(t, app, tab, "Debug login loop")
	if got := loadTopicTitleSource(tab.WorkspaceRoot, tab.TopicID); got != topicTitleSourceAuto {
		t.Fatalf("title source = %q, want auto", got)
	}
	// The attempt record lands right after the title write; poll for it.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if meta := loadTopicAutoTitleMeta(tab.WorkspaceRoot)[tab.TopicID]; meta.AIGenerated {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	meta := loadTopicAutoTitleMeta(tab.WorkspaceRoot)[tab.TopicID]
	if !meta.AIGenerated || meta.AIBasisHash == "" {
		t.Fatalf("auto-title meta = %+v, want AI-generated attempt recorded", meta)
	}
	if requests.Load() != 1 {
		t.Fatalf("title requests = %d, want 1", requests.Load())
	}
}

func TestGenerateAISessionTitleFailureKeepsPreviewAndRecordsAttempt(t *testing.T) {
	app, tab, sessionPath, requests := aiTitleFixture(t, "", true)
	// The text-derived path already wrote a truncated preview before the AI
	// request ran; the failed request must leave it untouched.
	if err := setTopicTitleWithSource(tab.WorkspaceRoot, tab.TopicID, "fix the login…", topicTitleSourceAuto); err != nil {
		t.Fatalf("seed preview title: %v", err)
	}

	app.maybeGenerateAISessionTitle(tab, tab.WorkspaceRoot, tab.TopicID, sessionPath)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if meta := loadTopicAutoTitleMeta(tab.WorkspaceRoot)[tab.TopicID]; meta.AIBasisHash != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	meta := loadTopicAutoTitleMeta(tab.WorkspaceRoot)[tab.TopicID]
	if meta.AIBasisHash == "" || meta.AIGenerated {
		t.Fatalf("auto-title meta = %+v, want failed attempt recorded", meta)
	}
	if got := loadTopicTitle(tab.WorkspaceRoot, tab.TopicID); got != "fix the login…" {
		t.Fatalf("preview title was overwritten to %q after failure", got)
	}
	if requests.Load() != 3 {
		t.Fatalf("title requests = %d, want 3 (Generate retries an empty result twice)", requests.Load())
	}
}

func TestGenerateAISessionTitleDoesNotRetrySameBasis(t *testing.T) {
	app, tab, sessionPath, requests := aiTitleFixture(t, "Debug login loop", false)
	app.maybeGenerateAISessionTitle(tab, tab.WorkspaceRoot, tab.TopicID, sessionPath)
	waitForAITitle(t, app, tab, "Debug login loop")

	// A later snapshot (e.g. an append) must not fire a second request for the
	// same first message.
	app.maybeGenerateAISessionTitle(tab, tab.WorkspaceRoot, tab.TopicID, sessionPath)
	time.Sleep(100 * time.Millisecond)
	if requests.Load() != 1 {
		t.Fatalf("title requests = %d, want 1 (no retry for same basis)", requests.Load())
	}
}

func TestGenerateAISessionTitleSkipsManualRename(t *testing.T) {
	app, tab, sessionPath, requests := aiTitleFixture(t, "Debug login loop", false)
	if err := setTopicTitleWithSource(tab.WorkspaceRoot, tab.TopicID, "My manual name", topicTitleSourceManual); err != nil {
		t.Fatalf("set manual title: %v", err)
	}
	app.maybeGenerateAISessionTitle(tab, tab.WorkspaceRoot, tab.TopicID, sessionPath)
	time.Sleep(100 * time.Millisecond)
	if got := loadTopicTitle(tab.WorkspaceRoot, tab.TopicID); got != "My manual name" {
		t.Fatalf("manual title was overwritten to %q", got)
	}
	if requests.Load() != 0 {
		t.Fatalf("title requests = %d, want 0 for manually named topic", requests.Load())
	}
}

func TestAIGeneratedTitleBlocksTextUpgrade(t *testing.T) {
	root := t.TempDir()
	app := &App{}
	app.recordAISessionTitleAttempt(root, "topic_ai_2", "first message", true)
	proposal := autoTopicTitleProposal{Stage: 3, UserTurns: 5, BasisHash: "something-else"}
	if shouldApplyAutoTopicTitle(root, "topic_ai_2", proposal) {
		t.Fatal("text-derived stage-3 upgrade applied over an AI-generated title")
	}
}
