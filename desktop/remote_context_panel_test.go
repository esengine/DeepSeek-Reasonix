package main

import (
	"context"
	"sync"
	"testing"

	"reasonix/internal/runtimeapi"
)

type remoteContextPanelRuntime struct {
	*remoteWorkbenchTestRuntime

	panelMu      sync.Mutex
	contextView  runtimeapi.ContextView
	changePages  map[runtimeapi.Cursor]runtimeapi.WorkspaceChangesPage
	changeInputs []runtimeapi.WorkspaceChangesInput
}

func (r *remoteContextPanelRuntime) SessionContext(_ context.Context, input runtimeapi.SessionContextInput) (runtimeapi.ContextView, error) {
	if input.Session != r.created.Session {
		return runtimeapi.ContextView{}, context.Canceled
	}
	return r.contextView, nil
}

func (r *remoteContextPanelRuntime) WorkspaceChanges(_ context.Context, input runtimeapi.WorkspaceChangesInput) (runtimeapi.WorkspaceChangesPage, error) {
	r.panelMu.Lock()
	defer r.panelMu.Unlock()
	r.changeInputs = append(r.changeInputs, input)
	return r.changePages[input.Cursor], nil
}

func TestRemoteContextPanelUsesHostContextAndExhaustsWorkspaceChanges(t *testing.T) {
	base := newRemoteWorkbenchTestRuntime()
	offset, limit := int64(12), int64(48)
	latest := int64(456)
	runtime := &remoteContextPanelRuntime{
		remoteWorkbenchTestRuntime: base,
		contextView: runtimeapi.ContextView{
			UsedTokens: 10, WindowTokens: 100, PromptTokens: 6, CompletionTokens: 4, TotalTokens: 30,
			ReasoningTokens: 2, CacheHitTokens: 3, CacheMissTokens: 1,
			SessionCacheHitTokens: 8, SessionCacheMissTokens: 5, SessionCompletionTokens: 17,
			RequestCount: 2, ElapsedMillis: 900, SessionCost: 0.25, SessionCurrency: "USD",
			Sources:   []runtimeapi.UsageSource{{Source: "remote-model", TotalTokens: 30, RequestCount: 2}},
			ReadFiles: []runtimeapi.ReadFileRecord{{Path: "README.md", Turn: 3, TimeMs: 123, Offset: &offset, Limit: &limit, Truncated: true}},
		},
		changePages: map[runtimeapi.Cursor]runtimeapi.WorkspaceChangesPage{
			"": {
				Files:        []runtimeapi.ChangedFile{{Path: "first.go", Sources: []runtimeapi.ChangeSource{runtimeapi.ChangeSession}, Turns: []int{1}}},
				GitAvailable: true, GitBranch: "remote-v1", HasMore: true, Next: "changes-page-2",
			},
			"changes-page-2": {
				Files:        []runtimeapi.ChangedFile{{Path: "second.go", OldPath: "old.go", Sources: []runtimeapi.ChangeSource{runtimeapi.ChangeGit}, GitStatus: "M", Turns: []int{2}, LatestPrompt: "finish", LatestTimeMillis: &latest}},
				GitAvailable: true, GitBranch: "remote-v1",
			},
		},
	}
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_context_panel", Label: "Host context panel"}
	app, _ := newRemoteWorkbenchTestApp(t, target, runtime, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote context"})
	if err != nil {
		t.Fatal(err)
	}

	view := app.ContextPanel(status.TabID)
	if view.UsedTokens != 10 || view.WindowTokens != 100 || view.RequestCount != 2 || view.ElapsedMs != 900 || view.SessionCost != 0.25 || view.SessionCurrency != "USD" {
		t.Fatalf("Remote context projection = %#v", view)
	}
	if len(view.ReadFiles) != 1 || view.ReadFiles[0].Path != "README.md" || view.ReadFiles[0].Offset != 12 || view.ReadFiles[0].Limit != 48 || !view.ReadFiles[0].Truncated {
		t.Fatalf("Remote read-file projection = %#v", view.ReadFiles)
	}
	if len(view.ChangedFiles) != 2 || view.ChangedFiles[0].Path != "first.go" || view.ChangedFiles[1].Path != "second.go" || view.ChangedFiles[1].LatestTime != latest {
		t.Fatalf("Remote changes projection = %#v", view.ChangedFiles)
	}
	runtime.panelMu.Lock()
	defer runtime.panelMu.Unlock()
	if len(runtime.changeInputs) != 2 || runtime.changeInputs[0].Cursor != "" || runtime.changeInputs[1].Cursor != "changes-page-2" {
		t.Fatalf("Remote changes pagination inputs = %#v", runtime.changeInputs)
	}
}
