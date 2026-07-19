package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/runtimeapi"
)

type remoteAuthorityPromptGateRuntime struct {
	*remoteWorkbenchTestRuntime

	started     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func newRemoteAuthorityPromptGateRuntime() *remoteAuthorityPromptGateRuntime {
	return &remoteAuthorityPromptGateRuntime{
		remoteWorkbenchTestRuntime: newRemoteWorkbenchTestRuntime(),
		started:                    make(chan struct{}, 1),
		release:                    make(chan struct{}),
	}
}

func (r *remoteAuthorityPromptGateRuntime) ApprovePrompt(ctx context.Context, input runtimeapi.ApproveInput) error {
	if err := r.remoteWorkbenchTestRuntime.ApprovePrompt(ctx, input); err != nil {
		return err
	}
	return r.waitForPromptRelease(ctx)
}

func (r *remoteAuthorityPromptGateRuntime) AnswerPrompt(ctx context.Context, input runtimeapi.AnswerInput) error {
	if err := r.remoteWorkbenchTestRuntime.AnswerPrompt(ctx, input); err != nil {
		return err
	}
	return r.waitForPromptRelease(ctx)
}

func (r *remoteAuthorityPromptGateRuntime) waitForPromptRelease(ctx context.Context) error {
	select {
	case r.started <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *remoteAuthorityPromptGateRuntime) releasePrompt() {
	r.releaseOnce.Do(func() { close(r.release) })
}

func TestRemotePromptSuccessDoesNotClearReplacementTargetPending(t *testing.T) {
	tests := []struct {
		name   string
		answer bool
		aba    bool
	}{
		{name: "approve_A_to_B"},
		{name: "approve_A_to_B_to_A", aba: true},
		{name: "answer_A_to_B", answer: true},
		{name: "answer_A_to_B_to_A", answer: true, aba: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targetA := TargetDescriptor{Kind: TargetRemote, ID: "host_prompt_authority_a", Label: "Host A"}
			targetB := TargetDescriptor{Kind: TargetRemote, ID: "host_prompt_authority_b", Label: "Host B"}
			promptID := runtimeapi.PromptID("prompt_reused_after_target_transition")
			gate := newRemoteAuthorityPromptGateRuntime()
			t.Cleanup(gate.releasePrompt)
			gate.snapshot.PendingPrompt = remoteAuthorityPendingPrompt(tc.answer, promptID)

			replacementA := &remoteWorkbenchTestAdapter{target: targetA, runtime: newRemoteWorkbenchTestRuntime()}
			replacementB := &remoteWorkbenchTestAdapter{target: targetB, runtime: newRemoteWorkbenchTestRuntime()}
			connector := TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
				switch {
				case sameTarget(target, targetA):
					return replacementA, nil
				case sameTarget(target, targetB):
					return replacementB, nil
				default:
					return nil, errors.New("unexpected target switch")
				}
			})
			app, manager := newRemoteWorkbenchTestApp(t, targetA, gate, connector)
			status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
				PrimaryDirectoryRef: "dir_primary",
				TopicTitle:          "Prompt authority",
			})
			if err != nil {
				t.Fatal(err)
			}
			initial := manager.Snapshot()

			result := make(chan error, 1)
			go func() {
				if tc.answer {
					result <- app.remoteAnswer(status.TabID, string(promptID), []QuestionAnswer{{
						QuestionID: "question_authority",
						Selected:   []string{"yes"},
					}})
					return
				}
				result <- app.remoteApprove(status.TabID, string(promptID), true, false, false)
			}()
			waitValue(t, gate.started, "Host A prompt RPC admission")

			if err := manager.Switch(context.Background(), targetB, SwitchTargetOptions{}); err != nil {
				t.Fatal(err)
			}
			if tc.aba {
				if err := manager.Switch(context.Background(), targetA, SwitchTargetOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			current := manager.Snapshot()
			wantTarget := targetB
			if tc.aba {
				wantTarget = targetA
			}
			if current.State != TargetRemoteConnected || !sameTarget(current.Target, wantTarget) || current.Generation == initial.Generation {
				t.Fatalf("replacement target = %#v, initial = %#v", current, initial)
			}
			installRemoteAuthorityPromptProjection(app, current, gate.workspace, gate.created, gate.snapshot, tc.answer, promptID)

			gate.releasePrompt()
			if err := waitValue(t, result, "Host A prompt RPC completion"); !errors.Is(err, ErrTargetTransitionSuperseded) {
				t.Fatalf("prompt completion error = %v, want ErrTargetTransitionSuperseded", err)
			}
			assertRemoteAuthorityPendingPrompt(t, app, current, gate.created.Session, tc.answer, promptID)
		})
	}
}

func remoteAuthorityPendingPrompt(answer bool, promptID runtimeapi.PromptID) *runtimeapi.PendingPrompt {
	if !answer {
		return &runtimeapi.PendingPrompt{
			Kind: runtimeapi.PromptApproval,
			Approval: &runtimeapi.ApprovalPrompt{
				ID: promptID, Tool: "bash", Subject: "run the replacement target test",
			},
		}
	}
	question := "Keep the replacement target pending prompt?"
	return &runtimeapi.PendingPrompt{
		Kind: runtimeapi.PromptAsk,
		Ask: &runtimeapi.AskPrompt{
			ID: promptID,
			Questions: []runtimeapi.AskQuestion{{
				ID: "question_authority", Prompt: &question,
			}},
		},
	}
}

func installRemoteAuthorityPromptProjection(
	app *App,
	target TargetManagerSnapshot,
	workspace runtimeapi.Workspace,
	created runtimeapi.CreatedSession,
	snapshot runtimeapi.SessionSnapshot,
	answer bool,
	promptID runtimeapi.PromptID,
) {
	snapshot = cloneRemoteSessionSnapshot(snapshot)
	snapshot.PendingPrompt = remoteAuthorityPendingPrompt(answer, promptID)
	tabID := remoteSessionTabID(created.Session)
	app.remote.workbenchMu.Lock()
	app.remote.workbench = remoteWorkbenchState{
		HostID:     target.Target.ID,
		Workspaces: map[runtimeapi.WorkspaceID]runtimeapi.Workspace{workspace.ID: workspace},
		Sessions: map[runtimeapi.SessionRef]*remoteWorkbenchSession{
			created.Session: {
				Created: created, Snapshot: snapshot, AttachedGeneration: target.Generation,
			},
		},
		SessionTabs: map[string]runtimeapi.SessionRef{tabID: created.Session},
		TabOrder:    []string{tabID},
		ActiveTabID: tabID,
	}
	app.remote.workbenchMu.Unlock()
}

func assertRemoteAuthorityPendingPrompt(
	t *testing.T,
	app *App,
	target TargetManagerSnapshot,
	ref runtimeapi.SessionRef,
	answer bool,
	promptID runtimeapi.PromptID,
) {
	t.Helper()
	app.remote.workbenchMu.RLock()
	defer app.remote.workbenchMu.RUnlock()
	current := app.remote.workbench.Sessions[ref]
	if app.remote.workbench.HostID != target.Target.ID || current == nil || current.AttachedGeneration != target.Generation {
		t.Fatalf("replacement projection = %#v, target = %#v", app.remote.workbench, target)
	}
	pending := current.Snapshot.PendingPrompt
	if pending == nil {
		t.Fatal("replacement target pending prompt was cleared by Host A RPC completion")
	}
	if answer {
		if pending.Kind != runtimeapi.PromptAsk || pending.Ask == nil || pending.Ask.ID != promptID {
			t.Fatalf("replacement ask prompt = %#v, want %q", pending, promptID)
		}
		return
	}
	if pending.Kind != runtimeapi.PromptApproval || pending.Approval == nil || pending.Approval.ID != promptID {
		t.Fatalf("replacement approval prompt = %#v, want %q", pending, promptID)
	}
}

type remoteAuthorityHostStateFixture struct {
	workbenchPendingKey string
	hostPendingKey      string
	otherHostID         string
	otherPendingKey     string
}

func TestSaveRemoteHostInvalidatesProjectionOnlyWhenAuthorityChanges(t *testing.T) {
	t.Run("label_only_preserves_projection", func(t *testing.T) {
		app, store := newRemoteAuthorityHostTestApp(t)
		configPath := filepath.Join(t.TempDir(), "ssh-config")
		created, err := app.SaveRemoteHost(RemoteHostInput{
			Mode: RemoteHostConnectionConfig, Alias: "authority-label", Label: "Before", SSHConfigPath: configPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		before, found, err := store.Get(created.ID)
		if err != nil || !found {
			t.Fatalf("load Host before label edit: found=%v err=%v", found, err)
		}
		fixture := seedRemoteAuthorityHostState(app, created.ID)

		updated, err := app.SaveRemoteHost(RemoteHostInput{
			ID: created.ID, Mode: created.Mode, Alias: created.Alias, Label: "After", SSHConfigPath: created.SSHConfigPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		after, found, err := store.Get(created.ID)
		if err != nil || !found {
			t.Fatalf("load Host after label edit: found=%v err=%v", found, err)
		}
		if updated.Label != "After" || remoteHostConnectionKey(before) != remoteHostConnectionKey(after) {
			t.Fatalf("label-only update changed connection authority: before=%#v after=%#v", before, after)
		}
		assertRemoteAuthorityHostState(t, app, created.ID, fixture, true)
	})

	authorityEdits := []struct {
		name   string
		create func(*testing.T) RemoteHostInput
		edit   func(*testing.T, RemoteHostView) RemoteHostInput
	}{
		{
			name: "config_alias",
			create: func(t *testing.T) RemoteHostInput {
				return RemoteHostInput{Mode: RemoteHostConnectionConfig, Alias: "authority-alias-a", Label: "Alias A"}
			},
			edit: func(t *testing.T, host RemoteHostView) RemoteHostInput {
				return RemoteHostInput{ID: host.ID, Mode: RemoteHostConnectionConfig, Alias: "authority-alias-c", Label: host.Label}
			},
		},
		{
			name: "config_path",
			create: func(t *testing.T) RemoteHostInput {
				return RemoteHostInput{Mode: RemoteHostConnectionConfig, Alias: "authority-config", Label: "Config A", SSHConfigPath: filepath.Join(t.TempDir(), "ssh-config-a")}
			},
			edit: func(t *testing.T, host RemoteHostView) RemoteHostInput {
				return RemoteHostInput{ID: host.ID, Mode: host.Mode, Alias: host.Alias, Label: host.Label, SSHConfigPath: filepath.Join(t.TempDir(), "ssh-config-c")}
			},
		},
		{
			name: "direct_destination",
			create: func(t *testing.T) RemoteHostInput {
				return RemoteHostInput{Mode: RemoteHostConnectionDirect, Destination: "builder@127.0.0.1", Port: 22, Label: "Direct A"}
			},
			edit: func(t *testing.T, host RemoteHostView) RemoteHostInput {
				return RemoteHostInput{ID: host.ID, Mode: host.Mode, Destination: "builder@127.0.0.2", Port: host.Port, Label: host.Label}
			},
		},
		{
			name: "direct_port",
			create: func(t *testing.T) RemoteHostInput {
				return RemoteHostInput{Mode: RemoteHostConnectionDirect, Destination: "builder@127.0.0.1", Port: 22, Label: "Port A"}
			},
			edit: func(t *testing.T, host RemoteHostView) RemoteHostInput {
				return RemoteHostInput{ID: host.ID, Mode: host.Mode, Destination: host.Destination, Port: 2222, Label: host.Label}
			},
		},
		{
			name: "connection_mode",
			create: func(t *testing.T) RemoteHostInput {
				return RemoteHostInput{Mode: RemoteHostConnectionConfig, Alias: "authority-mode-a", Label: "Mode A"}
			},
			edit: func(t *testing.T, host RemoteHostView) RemoteHostInput {
				return RemoteHostInput{ID: host.ID, Mode: RemoteHostConnectionDirect, Destination: "builder@127.0.0.3", Port: 22, Label: host.Label}
			},
		},
	}

	for _, tc := range authorityEdits {
		t.Run(tc.name, func(t *testing.T) {
			app, store := newRemoteAuthorityHostTestApp(t)
			created, err := app.SaveRemoteHost(tc.create(t))
			if err != nil {
				t.Fatal(err)
			}
			before, found, err := store.Get(created.ID)
			if err != nil || !found {
				t.Fatalf("load Host before authority edit: found=%v err=%v", found, err)
			}
			fixture := seedRemoteAuthorityHostState(app, created.ID)

			updated, err := app.SaveRemoteHost(tc.edit(t, created))
			if err != nil {
				t.Fatal(err)
			}
			after, found, err := store.Get(created.ID)
			if err != nil || !found {
				t.Fatalf("load Host after authority edit: found=%v err=%v", found, err)
			}
			if updated.ID != created.ID || remoteHostConnectionKey(before) == remoteHostConnectionKey(after) {
				t.Fatalf("test edit did not preserve ID and change authority: before=%#v after=%#v", before, after)
			}
			assertRemoteAuthorityHostState(t, app, created.ID, fixture, false)
		})
	}
}

func TestDeleteRemoteHostClearsProjectionAndHostPending(t *testing.T) {
	app, store := newRemoteAuthorityHostTestApp(t)
	created, err := app.SaveRemoteHost(RemoteHostInput{
		Mode: RemoteHostConnectionConfig, Alias: "authority-delete", Label: "Delete Host",
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedRemoteAuthorityHostState(app, created.ID)

	if err := app.DeleteRemoteHost(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(created.ID); err != nil || found {
		t.Fatalf("deleted Host lookup found=%v err=%v", found, err)
	}
	assertRemoteAuthorityHostState(t, app, created.ID, fixture, false)
}

func newRemoteAuthorityHostTestApp(t *testing.T) (*App, *RemoteHostStore) {
	t.Helper()
	store := newRemoteAppTestStore(t)
	localTarget := TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "Local"}
	local := &remoteWorkbenchTestAdapter{target: localTarget, runtime: newRemoteWorkbenchTestRuntime()}
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unexpected target connection")
	}), local, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)
	return app, store
}

func seedRemoteAuthorityHostState(app *App, hostID string) remoteAuthorityHostStateFixture {
	fixture := remoteAuthorityHostStateFixture{
		workbenchPendingKey: "workbench_pending_authority",
		hostPendingKey:      "host_pending_authority",
		otherHostID:         "host_pending_other",
		otherPendingKey:     "other_pending_authority",
	}
	ref := runtimeapi.SessionRef{WorkspaceID: "workspace_authority", SessionID: "session_authority"}
	workspace := runtimeapi.Workspace{ID: ref.WorkspaceID, Name: "Authority workspace", DisplayPath: "/srv/authority"}
	created := runtimeapi.CreatedSession{Session: ref, TopicID: "topic_authority", TopicTitle: "Authority topic"}
	projectedPending := &pendingRemoteSessionCreate{
		HostID: hostID, Fingerprint: fixture.workbenchPendingKey, Workspace: workspace, Created: created,
	}
	hostPending := &pendingRemoteSessionCreate{
		HostID: hostID, Fingerprint: fixture.hostPendingKey, Workspace: workspace, Created: created,
	}
	otherRef := runtimeapi.SessionRef{WorkspaceID: "workspace_other_authority", SessionID: "session_other_authority"}
	otherWorkspace := runtimeapi.Workspace{ID: otherRef.WorkspaceID, Name: "Other workspace", DisplayPath: "/srv/other"}
	otherPending := &pendingRemoteSessionCreate{
		HostID:      fixture.otherHostID,
		Fingerprint: fixture.otherPendingKey,
		Workspace:   otherWorkspace,
		Created:     runtimeapi.CreatedSession{Session: otherRef, TopicID: "topic_other_authority", TopicTitle: "Other topic"},
	}
	tabID := remoteSessionTabID(ref)

	app.remote.workbenchMu.Lock()
	app.remote.workbench = remoteWorkbenchState{
		HostID:     hostID,
		Workspaces: map[runtimeapi.WorkspaceID]runtimeapi.Workspace{workspace.ID: workspace},
		Sessions: map[runtimeapi.SessionRef]*remoteWorkbenchSession{
			ref: {Created: created, Snapshot: runtimeapi.SessionSnapshot{Session: ref, TopicID: created.TopicID, Title: created.TopicTitle}},
		},
		SessionTabs: map[string]runtimeapi.SessionRef{tabID: ref},
		TabOrder:    []string{tabID},
		ActiveTabID: tabID,
		Pending:     map[string]*pendingRemoteSessionCreate{fixture.workbenchPendingKey: projectedPending},
	}
	app.remote.workspacePending = map[string]map[string]*pendingRemoteSessionCreate{
		hostID: {
			fixture.hostPendingKey: hostPending,
		},
		fixture.otherHostID: {
			fixture.otherPendingKey: otherPending,
		},
	}
	app.remote.workbenchMu.Unlock()
	return fixture
}

func assertRemoteAuthorityHostState(
	t *testing.T,
	app *App,
	hostID string,
	fixture remoteAuthorityHostStateFixture,
	preserved bool,
) {
	t.Helper()
	app.remote.workbenchMu.RLock()
	defer app.remote.workbenchMu.RUnlock()
	state := app.remote.workbench
	hostPending := app.remote.workspacePending[hostID][fixture.hostPendingKey]
	otherPending := app.remote.workspacePending[fixture.otherHostID][fixture.otherPendingKey]
	if otherPending == nil || otherPending.HostID != fixture.otherHostID {
		t.Fatalf("unrelated Host pending was changed: %#v", app.remote.workspacePending)
	}
	if preserved {
		if state.HostID != hostID || len(state.Workspaces) != 1 || len(state.Sessions) != 1 ||
			len(state.SessionTabs) != 1 || len(state.TabOrder) != 1 || state.ActiveTabID == "" ||
			state.Pending[fixture.workbenchPendingKey] == nil || hostPending == nil {
			t.Fatalf("label-only edit did not preserve Host projection: state=%#v pending=%#v", state, app.remote.workspacePending)
		}
		return
	}
	if len(state.Workspaces) != 0 || len(state.Sessions) != 0 || len(state.SessionTabs) != 0 ||
		len(state.TabOrder) != 0 || state.ActiveTabID != "" || len(state.Pending) != 0 || hostPending != nil ||
		len(app.remote.workspacePending[hostID]) != 0 {
		t.Fatalf("stale Host projection survived authority removal: state=%#v pending=%#v", state, app.remote.workspacePending)
	}
}
