package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/provider"
)

type peIsolationHarness struct {
	mu               sync.Mutex
	plannerStarted   int
	bothPlannersSeen chan struct{}
	releasePlanners  chan struct{}
	executorInputs   []string
}

type peIsolationProvider struct {
	name    string
	harness *peIsolationHarness
}

func registerPEIsolationProvider(t *testing.T) (string, *peIsolationHarness) {
	t.Helper()
	kind := fmt.Sprintf(
		"desktop-pe-isolation-%s-%d",
		strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()),
		time.Now().UnixNano(),
	)
	h := &peIsolationHarness{
		bothPlannersSeen: make(chan struct{}),
		releasePlanners:  make(chan struct{}),
	}
	provider.Register(kind, func(cfg provider.Config) (provider.Provider, error) {
		return &peIsolationProvider{name: cfg.Name, harness: h}, nil
	})
	return kind, h
}

func (p *peIsolationProvider) Name() string { return p.name }

func (p *peIsolationProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	input := lastPEIsolationUser(req.Messages)
	switch p.name {
	case "planner":
		p.harness.markPlannerStarted()
		select {
		case <-p.harness.releasePlanners:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return peIsolationChunks(peIsolationPlanFor(input)), nil
	case "executor":
		p.harness.recordExecutorInput(input)
		return peIsolationChunks("done"), nil
	default:
		return nil, fmt.Errorf("unexpected provider %q", p.name)
	}
}

func (h *peIsolationHarness) markPlannerStarted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.plannerStarted++
	if h.plannerStarted == 2 {
		close(h.bothPlannersSeen)
	}
}

func (h *peIsolationHarness) recordExecutorInput(input string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.executorInputs = append(h.executorInputs, input)
}

func (h *peIsolationHarness) waitExecutorInputs(t *testing.T, want int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		got := append([]string(nil), h.executorInputs...)
		h.mu.Unlock()
		if len(got) >= want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	t.Fatalf("executor inputs = %d, want %d: %#v", len(h.executorInputs), want, h.executorInputs)
	return nil
}

func lastPEIsolationUser(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == provider.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

func peIsolationPlanFor(input string) string {
	switch {
	case strings.Contains(input, "alpha"):
		return "PLAN alpha: inspect alpha.go"
	case strings.Contains(input, "beta"):
		return "PLAN beta: inspect beta.go"
	default:
		return "PLAN unknown"
	}
}

func peIsolationChunks(text string) <-chan provider.Chunk {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch
}

func TestPlanExecuteTabsUnderSameProjectKeepPlannerHandoffsIsolated(t *testing.T) {
	isolateDesktopUserDirs(t)
	kind, harness := registerPEIsolationProvider(t)

	root := t.TempDir()
	configBody := fmt.Sprintf(`
default_model = "executor"

[agent]
planner_model = "planner"

[[providers]]
name = "executor"
kind = %q
base_url = "https://example.invalid/executor"
model = "executor-model"

[[providers]]
name = "planner"
kind = %q
base_url = "https://example.invalid/planner"
model = "planner-model"
`, kind, kind)
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	app := NewApp()
	alpha := app.createTabEntryWithID("project", root, "topic_alpha", "tab-alpha")
	beta := app.createTabEntryWithID("project", root, "topic_beta", "tab-beta")
	alpha.sink = &tabEventSink{tabID: alpha.ID, app: app}
	beta.sink = &tabEventSink{tabID: beta.ID, app: app}
	app.tabs[alpha.ID] = alpha
	app.tabs[beta.ID] = beta
	app.tabOrder = []string{alpha.ID, beta.ID}
	app.activeTabID = alpha.ID

	app.buildTabController(alpha)
	app.buildTabController(beta)
	if alpha.StartupErr != "" {
		t.Fatalf("alpha startup: %s", alpha.StartupErr)
	}
	if beta.StartupErr != "" {
		t.Fatalf("beta startup: %s", beta.StartupErr)
	}
	if alpha.Ctrl == nil || beta.Ctrl == nil {
		t.Fatalf("controllers not built: alpha=%v beta=%v", alpha.Ctrl != nil, beta.Ctrl != nil)
	}
	defer alpha.Ctrl.Close()
	defer beta.Ctrl.Close()

	app.SubmitToTab(alpha.ID, "implement alpha feature in alpha.go")
	app.SubmitToTab(beta.ID, "implement beta feature in beta.go")

	select {
	case <-harness.bothPlannersSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("both planners did not start")
	}
	close(harness.releasePlanners)

	inputs := harness.waitExecutorInputs(t, 2)
	var alphaInput, betaInput string
	for _, input := range inputs {
		switch {
		case strings.Contains(input, "Original task:\nimplement alpha"):
			alphaInput = input
		case strings.Contains(input, "Original task:\nimplement beta"):
			betaInput = input
		}
	}
	if alphaInput == "" || betaInput == "" {
		t.Fatalf("executor handoffs missing alpha or beta task: %#v", inputs)
	}
	if !strings.Contains(alphaInput, "PLAN alpha") || strings.Contains(alphaInput, "PLAN beta") {
		t.Fatalf("alpha executor received contaminated handoff:\n%s", alphaInput)
	}
	if !strings.Contains(betaInput, "PLAN beta") || strings.Contains(betaInput, "PLAN alpha") {
		t.Fatalf("beta executor received contaminated handoff:\n%s", betaInput)
	}
}
