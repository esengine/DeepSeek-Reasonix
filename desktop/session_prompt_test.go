package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type promptResumeCtrl struct {
	history []provider.Message
	resumed *agent.Session
	path    string
}

func (c *promptResumeCtrl) History() []provider.Message {
	return append([]provider.Message(nil), c.history...)
}

func (c *promptResumeCtrl) Resume(s *agent.Session, path string) {
	c.resumed = s
	c.path = path
}

func (c *promptResumeCtrl) SetSessionPath(path string) {
	c.path = path
}

func TestSessionWithFreshSystemPromptPreservesLoadedRewriteBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := agent.NewSession("old sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "read_file", Arguments: "{}"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "tool-1", Name: "read_file", Content: strings.Repeat("detail ", 100)})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	resumed := sessionWithFreshSystemPrompt(loaded, "new sys")
	msgs := resumed.Snapshot()
	msgs[3].Content = "[elided tool result]"
	resumed.Replace(msgs)
	if err := resumed.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite fresh-system resume: %v", err)
	}

	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := reloaded.Messages[0].Content; got != "new sys" {
		t.Fatalf("system prompt after rewrite = %q, want new sys", got)
	}
	if got := reloaded.Messages[3].Content; got != "[elided tool result]" {
		t.Fatalf("tool result after rewrite = %q, want elided", got)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after owned resume rewrite = %v err=%v, want none", matches, err)
	}
}

func TestMigratedSessionWithoutSystemPromptPersistsThroughDesktopSwitch(t *testing.T) {
	isolateDesktopUserDirs(t)
	src := t.TempDir()
	dest := t.TempDir()
	const legacy = `{"role":"user","content":"recovered after downgrade"}
{"role":"assistant","content":"legacy answer"}
`
	if err := os.WriteFile(filepath.Join(src, "desktop-legacy.jsonl"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}
	if n, err := agent.MigrateLegacySessions(src, dest, nil); err != nil || n != 1 {
		t.Fatalf("MigrateLegacySessions: n=%d err=%v", n, err)
	}

	path := filepath.Join(dest, "desktop-legacy.jsonl")
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession migrated: %v", err)
	}
	const freshSystem = "current deterministic system prompt"
	prov := &capturingProvider{}
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession(freshSystem), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Runner:       exec,
		Executor:     exec,
		SystemPrompt: freshSystem,
		SessionDir:   dest,
		SessionPath:  path,
		Label:        "migrated",
		Sink:         event.Discard,
	})
	defer ctrl.Close()
	resumeLoadedSessionAndGoal(ctrl, loaded, path, "")

	if history := ctrl.History(); len(history) == 0 || history[0].Role != provider.RoleSystem || history[0].Content != freshSystem {
		t.Fatalf("resumed history does not start with the fresh system prompt: %+v", history)
	}
	if err := ctrl.RunTurn(context.Background(), "new desktop turn"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	active := &WorkspaceTab{
		ID:          "legacy",
		Ctrl:        ctrl,
		Scope:       "global",
		SessionPath: path,
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	target := &WorkspaceTab{
		ID:          "target",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"legacy": active, "target": target},
		tabOrder:    []string{"legacy", "target"},
		activeTabID: "legacy",
	}
	if err := app.SetActiveTab("target"); err != nil {
		t.Fatalf("SetActiveTab: %v", err)
	}

	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after switch: %v", err)
	}
	got := reloaded.Snapshot()
	if len(got) != 5 {
		t.Fatalf("reloaded message count = %d, want 5: %+v", len(got), got)
	}
	if got[0].Role != provider.RoleSystem || got[0].Content != freshSystem {
		t.Fatalf("reloaded system prompt = %+v, want %q", got[0], freshSystem)
	}
	if got[3].Role != provider.RoleUser || agent.StripTransientUserBlocks(got[3].Content) != "new desktop turn" {
		t.Fatalf("reloaded new user turn = %+v", got[3])
	}
	if got[4].Role != provider.RoleAssistant || got[4].Content != "ok" {
		t.Fatalf("reloaded assistant turn = %+v", got[4])
	}
	if matches, err := filepath.Glob(filepath.Join(dest, "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after migrated switch = %v err=%v, want none", matches, err)
	}
}

func TestParallelDesktopTabsPersistCompleteTranscriptsAcrossReload(t *testing.T) {
	isolateDesktopUserDirs(t)
	const (
		tabCount    = 12
		turnsPerTab = 6
		systemText  = "stable desktop system prompt"
	)
	dir := t.TempDir()
	tabs := make(map[string]*WorkspaceTab, tabCount)
	tabOrder := make([]string, 0, tabCount)
	controllers := make([]*control.Controller, 0, tabCount)

	for tabIndex := range tabCount {
		id := fmt.Sprintf("parallel-%02d", tabIndex)
		path := filepath.Join(dir, id+".jsonl")
		prov := &capturingProvider{}
		exec := agent.New(prov, tool.NewRegistry(), agent.NewSession(systemText), agent.Options{}, event.Discard)
		ctrl := control.New(control.Options{
			Runner:       exec,
			Executor:     exec,
			SystemPrompt: systemText,
			SessionDir:   dir,
			SessionPath:  path,
			Label:        id,
			Sink:         event.Discard,
		})
		controllers = append(controllers, ctrl)
		tabOrder = append(tabOrder, id)
		tabs[id] = &WorkspaceTab{
			ID:          id,
			Ctrl:        ctrl,
			Scope:       "global",
			SessionPath: path,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		}
	}
	app := &App{
		tabs:        tabs,
		tabOrder:    tabOrder,
		activeTabID: tabOrder[0],
	}

	start := make(chan struct{})
	errs := make(chan error, tabCount+1)
	var wg sync.WaitGroup
	for tabIndex, ctrl := range controllers {
		wg.Go(func() {
			<-start
			for turn := range turnsPerTab {
				input := fmt.Sprintf("tab-%02d-turn-%02d", tabIndex, turn)
				if err := ctrl.RunTurn(context.Background(), input); err != nil {
					errs <- fmt.Errorf("%s: %w", input, err)
					return
				}
			}
		})
	}
	wg.Go(func() {
		<-start
		for range 3 {
			for _, id := range tabOrder {
				if err := app.SetActiveTab(id); err != nil {
					errs <- fmt.Errorf("switch to %s: %w", id, err)
					return
				}
			}
		}
	})

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		for _, ctrl := range controllers {
			ctrl.Close()
		}
		return
	}

	for _, ctrl := range controllers {
		if err := ctrl.Snapshot(); err != nil {
			t.Fatalf("final snapshot %s: %v", ctrl.Label(), err)
		}
		ctrl.Close()
	}

	for tabIndex, id := range tabOrder {
		path := tabs[id].SessionPath
		reloaded, err := agent.LoadSession(path)
		if err != nil {
			t.Fatalf("LoadSession %s: %v", id, err)
		}
		msgs := reloaded.Snapshot()
		wantMessages := 1 + turnsPerTab*2
		if len(msgs) != wantMessages {
			t.Fatalf("%s message count = %d, want %d: %+v", id, len(msgs), wantMessages, msgs)
		}
		if msgs[0].Role != provider.RoleSystem || msgs[0].Content != systemText {
			t.Fatalf("%s system message = %+v", id, msgs[0])
		}
		for turn := range turnsPerTab {
			user := msgs[1+turn*2]
			assistant := msgs[2+turn*2]
			wantUser := fmt.Sprintf("tab-%02d-turn-%02d", tabIndex, turn)
			if user.Role != provider.RoleUser || agent.StripTransientUserBlocks(user.Content) != wantUser {
				t.Fatalf("%s turn %d user = %+v, want %q", id, turn, user, wantUser)
			}
			if assistant.Role != provider.RoleAssistant || assistant.Content != "ok" {
				t.Fatalf("%s turn %d assistant = %+v, want ok", id, turn, assistant)
			}
		}
	}
}

func TestResumeWithFreshSystemPromptPreservesLoadedRewriteBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := agent.NewSession("old sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "read_file", Arguments: "{}"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "tool-1", Name: "read_file", Content: strings.Repeat("detail ", 100)})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	ctrl := &promptResumeCtrl{history: []provider.Message{{Role: provider.RoleSystem, Content: "new sys"}}}
	resumeWithFreshSystemPrompt(ctrl, loaded.Snapshot(), path)
	if ctrl.resumed == nil {
		t.Fatalf("Resume was not called")
	}

	msgs := ctrl.resumed.Snapshot()
	msgs[3].Content = "[elided tool result]"
	ctrl.resumed.Replace(msgs)
	if err := ctrl.resumed.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite resumed history: %v", err)
	}

	if got := ctrl.path; got != path {
		t.Fatalf("resume path = %q, want %q", got, path)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := reloaded.Messages[0].Content; got != "new sys" {
		t.Fatalf("system prompt after rewrite = %q, want new sys", got)
	}
	if got := reloaded.Messages[3].Content; got != "[elided tool result]" {
		t.Fatalf("tool result after rewrite = %q, want elided", got)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after resume rewrite = %v err=%v, want none", matches, err)
	}
}

func TestResumeWithFreshSystemPromptRejectsStaleCarriedHistoryBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := agent.NewSession("old sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk two"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := []provider.Message{
		{Role: provider.RoleSystem, Content: "old sys"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "one"},
	}
	ctrl := &promptResumeCtrl{history: []provider.Message{{Role: provider.RoleSystem, Content: "new sys"}}}
	resumeWithFreshSystemPrompt(ctrl, stale, path)
	if ctrl.resumed == nil {
		t.Fatalf("Resume was not called")
	}
	if err := ctrl.resumed.SaveRewrite(path); !errors.Is(err, agent.ErrSessionSnapshotConflict) {
		t.Fatalf("SaveRewrite stale carried history err = %v, want ErrSessionSnapshotConflict", err)
	}

	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession current: %v", err)
	}
	if got := reloaded.Messages[len(reloaded.Messages)-1].Content; got != "disk two" {
		t.Fatalf("original tail after stale resume rewrite = %q, want disk two", got)
	}
}
