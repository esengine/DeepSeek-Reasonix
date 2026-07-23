package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/workbench/client"
	"reasonix/internal/remote/workbench/mirror"
	"reasonix/internal/remote/workbench/target"
)

// This file is the native adapter between the frozen Remote protocol and the
// existing main-window AppBindings. Remote is an execution target, not a
// second application: the same React tree hydrates from these projections and
// receives the same agent:event stream it already uses for Local sessions.

func (a *App) workbenchProjectionTabID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.tabByIDLocked(""); tab != nil {
		return tab.ID
	}
	return a.activeTabID
}

func (a *App) workbenchClientCallbacks(generation uint64, tabID string) client.Callbacks {
	return client.Callbacks{
		OnSessionEvent: func(notification protocol.SessionEvent) {
			k := a.workbench()
			busyChanged := false
			busy := false
			k.mu.Lock()
			if k.remoteGen != generation {
				k.mu.Unlock()
				return
			}
			k.snapshot.BoundarySeq = notification.Seq
			k.snapshot.Runtime.LiveEvents = append(k.snapshot.Runtime.LiveEvents, notification.Event)
			switch notification.Event.Kind {
			case "turn_started":
				busyChanged, busy = true, true
				k.snapshot.Runtime.Running = true
				k.snapshot.Runtime.CurrentTurn = &protocol.TurnState{TurnID: notification.TurnID}
			case "turn_done":
				busyChanged, busy = true, false
				k.snapshot.Runtime.Running = false
				k.snapshot.Runtime.CurrentTurn = nil
				k.snapshot.Runtime.CancelRequested = false
				k.snapshot.PendingPrompt = nil
			case "approval_request":
				if approval := notification.Event.Approval; approval != nil {
					reason := approval.Reason
					k.snapshot.PendingPrompt = &protocol.PendingPrompt{Kind: protocol.PromptApproval, Approval: &protocol.ApprovalPrompt{
						PromptID: protocol.PromptID(approval.ID), Tool: approval.Tool, Subject: approval.Subject,
						Reason: &reason, Fresh: approval.Fresh,
						AllowedDecisions: []protocol.PromptDecision{protocol.DecisionAllowOnce, protocol.DecisionAllowSession, protocol.DecisionAllowPersistent, protocol.DecisionDeny},
					}}
				}
			case "ask_request":
				if ask := notification.Event.Ask; ask != nil {
					k.snapshot.PendingPrompt = &protocol.PendingPrompt{Kind: protocol.PromptAsk, Ask: &protocol.AskPrompt{PromptID: protocol.PromptID(ask.ID)}}
				}
			}
			k.mu.Unlock()
			if busyChanged {
				k.targets.SetRemoteBusy(busy)
			}
			k.transitionMu.Lock()
			active, identityGen, requestSeq := k.targets.Active()
			visible := active.Kind == target.KindRemote && active.HostID != "" && active.Workspace != ""
			if !visible {
				switch notification.Event.Kind {
				case "approval_request", "ask_request", "turn_done":
					a.emitWorkbenchTarget("background_activity", active, identityGen, requestSeq, "Remote session has background activity.")
				}
				k.transitionMu.Unlock()
				return
			}
			k.mu.Lock()
			projectionTabID := k.remoteTabID
			generationCurrent := k.remoteGen == generation
			k.mu.Unlock()
			if !generationCurrent {
				k.transitionMu.Unlock()
				return
			}
			if projectionTabID == "" {
				projectionTabID = tabID
			}
			if a.ctx != nil {
				a.runtimeEvents.Emit(a.ctx, "agent:event", wireEventTab{Event: notification.Event, TabID: projectionTabID})
			}
			k.transitionMu.Unlock()
			if notification.Event.Kind == "turn_done" {
				go a.workbenchRefreshSnapshot(generation, projectionTabID)
			}
		},
		OnResyncRequired: func(protocol.SessionResyncRequired) {
			go a.workbenchRefreshSnapshot(generation, tabID)
		},
		OnCatalogChanged: func(protocol.CatalogChanged) {
			go a.workbenchRefreshCatalog(generation)
		},
		OnDisconnected: func() {
			k := a.workbench()
			id, identityGen, requestSeq, changed := k.targets.MarkRemoteDisconnected(generation)
			if !changed {
				return
			}
			k.mu.Lock()
			if k.remoteGen != generation {
				k.mu.Unlock()
				return
			}
			projectionTabID := k.remoteTabID
			if projectionTabID == "" {
				projectionTabID = tabID
			}
			k.remote = nil
			k.remoteGen = 0
			k.remoteTabID = ""
			k.remoteFingerprint = ""
			k.providerAccess = nil
			k.snapshot = protocol.SessionSnapshot{}
			k.catalog = protocol.WorkspaceCatalogResult{}
			k.sessionCatalog = protocol.SessionCatalogResult{}
			k.mu.Unlock()
			a.emitWorkbenchTarget("disconnected", id, identityGen, requestSeq, "Remote transport closed; reconnect to resume the Host session.")
			a.emitReady(a.ctx, projectionTabID)
			a.emitRuntimeEvent("runtime:rebuilt", projectionTabID)
		},
	}
}

func (a *App) activeRemoteWorkbench() (*client.Client, protocol.SessionSnapshot, protocol.WorkspaceCatalogResult, string, bool) {
	k := a.workbench()
	id, _, _ := k.targets.Active()
	if id.Kind != target.KindRemote {
		return nil, protocol.SessionSnapshot{}, protocol.WorkspaceCatalogResult{}, "", false
	}
	remoteState := k.targets.Remote()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.remote == nil || remoteState == nil || !remoteState.Connected || k.remoteGen != remoteState.Generation {
		return nil, protocol.SessionSnapshot{}, protocol.WorkspaceCatalogResult{}, "", false
	}
	return k.remote, k.snapshot, k.catalog, k.remoteTabID, true
}

func (a *App) activeWorkbenchTargetIsRemote() bool {
	id, _, _ := a.workbench().targets.Active()
	return id.Kind == target.KindRemote
}

func remoteSavedSessionManagementErr() error {
	return fmt.Errorf("CAPABILITY_UNAVAILABLE: Remote saved-session management is not available in V1")
}

func remoteMemoryUnavailableErr() error {
	return fmt.Errorf("CAPABILITY_UNAVAILABLE: Remote Memory is not available in V1")
}

func remoteAutoResearchUnavailableErr() error {
	return fmt.Errorf("CAPABILITY_UNAVAILABLE: Remote Auto Research is not available in V1")
}

func (a *App) workbenchSnapshot() (protocol.SessionSnapshot, bool) {
	_, snapshot, _, _, ok := a.activeRemoteWorkbench()
	return snapshot, ok
}

func (a *App) workbenchProjectTabMetas(metas []TabMeta) []TabMeta {
	_, snapshot, _, tabID, ok := a.activeRemoteWorkbench()
	if !ok {
		return metas
	}
	id, _, _ := a.workbench().targets.Active()
	for i := range metas {
		if metas[i].ID != tabID {
			continue
		}
		metas[i].WorkspaceRoot = id.Workspace
		metas[i].WorkspacePath = id.Workspace
		metas[i].WorkspaceName = workbenchBaseName(id.Workspace)
		metas[i].Cwd = id.Workspace
		metas[i].TopicID = string(snapshot.Meta.TopicID)
		metas[i].TopicTitle = snapshot.Meta.Title
		metas[i].Label = snapshot.Meta.ResolvedProfile.Model
		metas[i].Ready = true
		metas[i].Running = snapshot.Runtime.Running
		metas[i].PendingPrompt = snapshot.PendingPrompt != nil
		metas[i].BackgroundJobs = len(snapshot.Jobs)
		metas[i].CancelRequested = snapshot.Runtime.CancelRequested
		metas[i].Cancellable = snapshot.Runtime.CurrentTurn != nil || snapshot.Runtime.CurrentOperation != nil
		metas[i].Mode = string(snapshot.Meta.ResolvedProfile.CollaborationMode)
		metas[i].CollaborationMode = string(snapshot.Meta.ResolvedProfile.CollaborationMode)
		metas[i].ToolApprovalMode = string(snapshot.Meta.ResolvedProfile.ToolApprovalMode)
		metas[i].TokenMode = string(snapshot.Meta.ResolvedProfile.TokenMode)
		metas[i].Goal = workbenchString(snapshot.Meta.Goal)
		metas[i].GoalStatus = string(snapshot.Meta.GoalStatus)
	}
	return metas
}

func workbenchBaseName(path string) string {
	path = strings.TrimRight(strings.ReplaceAll(path, "\\", "/"), "/")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func workbenchLoadCatalog(ctx context.Context, cli *client.Client) (protocol.WorkspaceCatalogResult, error) {
	raw, err := cli.Request(ctx, string(protocol.MethodCatalogWorkspace), protocol.WorkspaceCatalogParams{})
	if err != nil {
		return protocol.WorkspaceCatalogResult{}, err
	}
	decoded, err := protocol.DecodeResult(protocol.MethodCatalogWorkspace, raw)
	if err != nil {
		return protocol.WorkspaceCatalogResult{}, err
	}
	return decoded.(protocol.WorkspaceCatalogResult), nil
}

func workbenchLoadSessionCatalog(ctx context.Context, cli *client.Client) (protocol.SessionCatalogResult, error) {
	raw, err := cli.Request(ctx, string(protocol.MethodCatalogSession), protocol.SessionCatalogParams{})
	if err != nil {
		return protocol.SessionCatalogResult{}, err
	}
	decoded, err := protocol.DecodeResult(protocol.MethodCatalogSession, raw)
	if err != nil {
		return protocol.SessionCatalogResult{}, err
	}
	return decoded.(protocol.SessionCatalogResult), nil
}

func (a *App) workbenchSessionCatalog() (protocol.SessionCatalogResult, bool) {
	k := a.workbench()
	id, _, _ := k.targets.Active()
	if id.Kind != target.KindRemote {
		return protocol.SessionCatalogResult{}, false
	}
	remoteState := k.targets.Remote()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.remote == nil || remoteState == nil || !remoteState.Connected || k.remoteGen != remoteState.Generation {
		return protocol.SessionCatalogResult{}, false
	}
	return k.sessionCatalog, true
}

func (a *App) workbenchRefreshCatalog(generation uint64) {
	cli, _, _, _, ok := a.activeRemoteWorkbench()
	if !ok || cli.Generation() != generation {
		return
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 10*time.Second)
	defer cancel()
	catalog, err := workbenchLoadCatalog(ctx, cli)
	if err != nil {
		return
	}
	sessionCatalog, err := workbenchLoadSessionCatalog(ctx, cli)
	if err != nil {
		return
	}
	k := a.workbench()
	k.mu.Lock()
	if k.remote == cli && k.remoteGen == generation {
		k.catalog = catalog
		k.sessionCatalog = sessionCatalog
	}
	k.mu.Unlock()
}

func (a *App) workbenchRefreshSnapshot(generation uint64, tabID string) {
	cli, _, _, _, ok := a.activeRemoteWorkbench()
	if !ok || cli.Generation() != generation {
		return
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 15*time.Second)
	defer cancel()
	_, err := cli.SubscribeCommitted(ctx, protocol.HistoryMaxTurns, func(result protocol.SessionSubscribeResult) error {
		k := a.workbench()
		k.transitionMu.Lock()
		defer k.transitionMu.Unlock()
		active, _, _ := k.targets.Active()
		if active.Kind != target.KindRemote {
			return nil
		}
		k.mu.Lock()
		if k.remote == cli && k.remoteGen == generation {
			k.snapshot = result.Snapshot
			if k.remoteTabID != "" {
				tabID = k.remoteTabID
			}
		} else {
			k.mu.Unlock()
			return nil
		}
		k.mu.Unlock()
		go a.workbenchMirrorSnapshot(cli, result.Snapshot)
		a.emitReady(a.ctx, tabID)
		a.emitRuntimeEvent("runtime:rebuilt", tabID)
		return nil
	})
	if err != nil {
		return
	}
}

func (a *App) workbenchMirrorSnapshot(cli *client.Client, snapshot protocol.SessionSnapshot) {
	var artifact *protocol.ExternalizedField
	for i := range snapshot.Externalized {
		if snapshot.Externalized[i].JSONPointer == "/mirror/session.jsonl" {
			copy := snapshot.Externalized[i]
			artifact = &copy
			break
		}
	}
	if artifact == nil || artifact.TotalBytes <= 0 || artifact.TotalBytes > protocol.ContentRefObjectBytes {
		return
	}
	k := a.workbench()
	k.mu.Lock()
	if k.remote != cli {
		k.mu.Unlock()
		return
	}
	fingerprint := k.remoteFingerprint
	k.mu.Unlock()
	id, _, _ := k.targets.Active()
	if fingerprint == "" || id.Kind != target.KindRemote || id.Workspace == "" {
		return
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 30*time.Second)
	defer cancel()
	data := make([]byte, 0, artifact.TotalBytes)
	var offset int64
	for {
		raw, err := cli.Request(ctx, string(protocol.MethodSessionContent), protocol.SessionContentParams{ContentRef: artifact.ContentRef, Offset: offset})
		if err != nil {
			return
		}
		decoded, err := protocol.DecodeResult(protocol.MethodSessionContent, raw)
		if err != nil {
			return
		}
		chunk := decoded.(protocol.SessionContentResult)
		bytes, err := base64.StdEncoding.DecodeString(chunk.DataBase64)
		if err != nil {
			return
		}
		data = append(data, bytes...)
		if chunk.NextOffset == nil {
			break
		}
		offset = *chunk.NextOffset
	}
	if int64(len(data)) != artifact.TotalBytes {
		return
	}
	sum := sha256.Sum256(data)
	contentSHA := hex.EncodeToString(sum[:])
	if contentSHA != artifact.SHA256 {
		return
	}
	artifacts := map[string][]byte{"session.jsonl": data}
	_ = (mirror.Store{}).ApplyCheckpoint(fingerprint, id.Workspace, mirror.Manifest{
		SessionID: string(snapshot.Target.SessionID), Revision: time.Now().UnixMilli(),
		Digest: mirror.DigestArtifacts(artifacts), ModelRef: snapshot.Meta.ResolvedProfile.Model,
		Label: snapshot.Meta.Title, ArtifactSHA: map[string]string{"session.jsonl": contentSHA},
	}, artifacts)
}

func (a *App) workbenchRequest(method protocol.Method, params any) ([]byte, error) {
	cli, _, _, _, ok := a.activeRemoteWorkbench()
	if !ok {
		return nil, fmt.Errorf("CAPABILITY_UNAVAILABLE: active target is local")
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 30*time.Second)
	defer cancel()
	return cli.Request(ctx, string(method), params)
}

func (a *App) workbenchSubmit(input, display, editedOriginal string, invocations []protocol.Invocation, recovery bool) (bool, error) {
	if a.workbench().targets.Connecting() {
		return true, fmt.Errorf("Remote target is connecting; wait for the connection to finish")
	}
	cli, _, _, _, ok := a.activeRemoteWorkbench()
	if !ok {
		return false, nil
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return true, fmt.Errorf("input is required")
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 30*time.Second)
	defer cancel()
	_, err := cli.Request(ctx, string(protocol.MethodSessionSubmit), protocol.SessionSubmitParams{
		Input: input, DisplayText: display, EditedOriginal: editedOriginal,
		Invocations: invocations, DeliveryRecovery: recovery,
	})
	return true, err
}

func (a *App) workbenchSetProfile(patch protocol.ProfilePatch) (bool, error) {
	cli, _, _, tabID, ok := a.activeRemoteWorkbench()
	if !ok {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 30*time.Second)
	defer cancel()
	if _, err := cli.SetProfile(ctx, patch); err != nil {
		return true, err
	}
	a.workbenchRefreshSnapshot(cli.Generation(), tabID)
	return true, nil
}
