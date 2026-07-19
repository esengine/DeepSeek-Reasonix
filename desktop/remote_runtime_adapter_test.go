package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/eventwire"
	remoteclient "reasonix/internal/remote/client"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/runtimeapi"
)

const remoteAdapterTestTimeout = 5 * time.Second

type remoteAdapterPeerScript func(*rpcwire.Conn, net.Conn)

type remoteAdapterScriptedFactory struct {
	mu      sync.Mutex
	scripts []remoteAdapterPeerScript
}

func (f *remoteAdapterScriptedFactory) Open(context.Context) (remoteclient.Transport, error) {
	f.mu.Lock()
	if len(f.scripts) == 0 {
		f.mu.Unlock()
		return nil, errors.New("no scripted Remote peer")
	}
	script := f.scripts[0]
	f.scripts = f.scripts[1:]
	f.mu.Unlock()
	clientSide, serverSide := net.Pipe()
	wire := rpcwire.NewConn(serverSide, serverSide, rpcwire.Options{
		Name: "remote-runtime-adapter-test", MaxInboundBytes: protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes, StrictJSONRPC: true,
	})
	script(wire, serverSide)
	go func() { _ = wire.Serve(context.Background()) }()
	return clientSide, nil
}

func remoteAdapterTestBuildID() protocol.BuildID {
	return protocol.BuildID{
		ProductVersion: "adapter-test", SourceRevision: strings.Repeat("a", 40),
		ProtocolVersion: protocol.ProtocolVersion, SchemaHash: protocol.SchemaHash(),
	}
}

func remoteAdapterInitialize(buildID protocol.BuildID, lease protocol.LeaseID) protocol.InitializeResult {
	return protocol.InitializeResult{
		BuildID: buildID, HostEpoch: "host-epoch", Lease: protocol.LeaseInfo{
			LeaseID: lease, TTLMillis: protocol.LeaseTTLMillis, PingIntervalMs: protocol.LeasePingIntervalMillis,
		},
		Host:         protocol.HostInfo{OS: "linux", Arch: "amd64", ShellKind: "bash", SandboxBackend: "landlock"},
		Capabilities: protocol.FrozenCapabilities(false, false),
	}
}

func remoteAdapterBasePeer(
	buildID protocol.BuildID,
	lease protocol.LeaseID,
	wantResume protocol.LeaseID,
	configure func(*rpcwire.Conn, net.Conn),
) remoteAdapterPeerScript {
	return func(wire *rpcwire.Conn, raw net.Conn) {
		wire.Handle(string(protocol.MethodRemoteInitialize), func(_ context.Context, payload json.RawMessage) (any, error) {
			var params protocol.InitializeParams
			if err := json.Unmarshal(payload, &params); err != nil {
				return nil, err
			}
			if params.BuildID != buildID || params.ResumeLeaseID != wantResume {
				return nil, fmt.Errorf("initialize identity = %+v", params)
			}
			return remoteAdapterInitialize(buildID, lease), nil
		})
		wire.Handle(string(protocol.MethodRemotePing), func(context.Context, json.RawMessage) (any, error) {
			return protocol.PingResult{HostEpoch: "host-epoch", LeaseTTL: protocol.LeaseTTLMillis}, nil
		})
		wire.Handle(string(protocol.MethodHostCapabilities), func(context.Context, json.RawMessage) (any, error) {
			return protocol.HostCapabilitiesResult{HostEpoch: "host-epoch", Capabilities: protocol.FrozenCapabilities(false, false)}, nil
		})
		wire.Handle(string(protocol.MethodHostConfigSummary), func(context.Context, json.RawMessage) (any, error) {
			return protocol.HostConfigSummaryResult{
				Revision:        "catalog-1",
				EffectiveScopes: []protocol.EffectiveScope{{Name: "workspace", Active: true}},
				DisplayPaths:    []protocol.ConfigDisplayPath{{Scope: "workspace", DisplayPath: "/srv/repo/.codex/config.toml"}},
				FeatureStates:   []protocol.FeatureState{{Feature: "memory", Available: false, Summary: "disabled"}},
				CLIHints:        []protocol.CLIHint{{Label: "Inspect Remote Host", Command: "reasonix remote doctor"}},
			}, nil
		})
		if configure != nil {
			configure(wire, raw)
		}
	}
}

func remoteAdapterSnapshot(
	target protocol.RuntimeTarget,
	runtimeEpoch protocol.RuntimeEpoch,
	snapshotID protocol.SnapshotID,
	boundary uint64,
	prompt *protocol.PendingPrompt,
) protocol.SessionSnapshot {
	goal := "ship Remote V1"
	lastError := "recoverable warning"
	toolArguments := `{"command":"go test ./..."}`
	toolSummary := "tests"
	toolDiff := "+green"
	toolError := "archived result"
	todo := "verify reconnect"
	checkpointPrompt := "implement Remote"
	offset := int64(3)
	limit := int64(8)
	return protocol.SessionSnapshot{
		SnapshotID: snapshotID, HostEpoch: "host-epoch", Target: target, RuntimeEpoch: runtimeEpoch, BoundarySeq: boundary,
		Meta: protocol.SessionMetaSnapshot{
			TopicID: "topic-opaque", Title: "Remote Session", Goal: &goal, GoalStatus: protocol.GoalRunning,
			ResolvedProfile: protocol.ResolvedProfile{
				Model: "provider/model", Effort: "high", CollaborationMode: protocol.CollaborationGoal,
				TokenMode: protocol.TokenEconomy, ToolApprovalMode: protocol.ToolApprovalAsk,
			},
			Capabilities: protocol.FrozenCapabilities(false, false),
		},
		Runtime: protocol.SessionRuntimeState{
			LastError: &lastError, LastOutcome: protocol.OutcomeInterrupted,
			Interruption: &protocol.RuntimeInterruption{PreviousTurnInterrupted: true, Reason: protocol.InterruptionHostRestarted},
			LiveEvents:   []eventwire.Event{{Kind: "notice", Text: "restored"}},
		},
		History: protocol.HistoryPage{
			SnapshotID: snapshotID, StartTurn: 0, EndTurn: 1, TotalTurns: 1, ActualTurns: 1,
			Messages: []protocol.HistoryMessage{{
				Role: "assistant", Content: stringPointer("answer"), Detail: stringPointer("detail"), Code: "notice",
				SubmitText: stringPointer("submit"), CheckpointID: "checkpoint-opaque", CreatedAtMs: 101,
				Reasoning: stringPointer("reasoning"), WorkDurationMs: 202,
				MemoryCitations: []eventwire.MemoryCitation{{ID: "memory-1", Source: "memory.md", LineStart: 1, LineEnd: 2}},
				Level:           "warn", ToolCalls: []protocol.HistoryToolCall{{
					ID: "tool-1", Name: "bash", Arguments: &toolArguments, Subject: "go test", Summary: &toolSummary,
					Diff: &toolDiff, Added: 1, Removed: 2, ArgumentsArchived: true,
				}},
				ToolCallID: "tool-1", ToolName: "bash", ToolResultArchived: true, ToolResultError: &toolError,
				Pending: true, Trigger: "manual", Messages: 3, Summary: stringPointer("summary"), Archive: stringPointer("archive"),
			}},
			Externalized: []protocol.ExternalizedField{},
		},
		PendingPrompt: prompt,
		Todos:         []protocol.TodoItem{{Content: &todo, Status: protocol.TodoInProgress, ActiveForm: "verifying", Level: 2}},
		Context: protocol.ContextView{
			UsedTokens: 10, WindowTokens: 100, PromptTokens: 11, CompletionTokens: 12, TotalTokens: 23,
			ReasoningTokens: 4, CacheHitTokens: 5, CacheMissTokens: 6, SessionCacheHitTokens: 7,
			SessionCacheMissTokens: 8, SessionCompletionTokens: 9, RequestCount: 2, ElapsedMs: 303,
			SessionCost: 1.25, SessionCurrency: "$", Sources: []protocol.UsageSourceView{{
				Source: "provider", PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3, ReasoningTokens: 1,
				CacheHitTokens: 1, CacheMissTokens: 1, RequestCount: 1, SessionCost: .5, SessionCurrency: "$",
			}},
			ReadFiles: []protocol.ReadFileRecord{{Path: "main.go", Turn: 1, TimeMs: 404, Offset: &offset, Limit: &limit, Truncated: true}},
		},
		Jobs: []protocol.JobView{{ID: "job-opaque", Kind: protocol.JobBash, Label: "tests", Status: protocol.JobRunning, StartedAt: 505}},
		Checkpoints: []protocol.CheckpointView{{
			CheckpointID: "checkpoint-opaque", DisplayTurn: 1, Prompt: &checkpointPrompt, Files: []string{"main.go"},
			FileCount: 1, CreatedAtMs: 606, CanCode: true, CanConversation: true,
		}},
		Externalized: []protocol.ExternalizedField{},
	}
}

func stringPointer(value string) *string { return &value }

func remoteAdapterAskPrompt() *protocol.PendingPrompt {
	prompt := "Which state?"
	description := "Recovered from the Host"
	return &protocol.PendingPrompt{
		Kind: protocol.PromptAsk,
		Ask: &protocol.AskPrompt{PromptID: "prompt-ask", Questions: []protocol.AskQuestion{{
			QuestionID: "question-1", Header: "Recovery", Prompt: &prompt,
			Options: []protocol.AskOption{{Label: "Fresh", Description: &description}}, Multi: true,
		}}},
	}
}

func newRemoteAdapterTestConnector(
	t *testing.T,
	factory remoteclient.TransportFactory,
) (*RemoteTargetConnector, *RemoteHostStore, RemoteHostEntry) {
	t.Helper()
	store, err := NewRemoteHostStore(filepath.Join(t.TempDir(), "remote-hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := NewRemoteHostEntry("linux-test", "Linux Test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	buildID := remoteAdapterTestBuildID()
	connector := &RemoteTargetConnector{
		store:   store,
		buildID: func() (protocol.BuildID, error) { return buildID, nil },
		newClient: func(saved RemoteHostEntry, actual protocol.BuildID) (*remoteclient.Client, error) {
			return remoteclient.New(remoteclient.Options{
				Factory: factory, BuildID: actual, ClientInstanceID: protocol.ClientInstanceID(saved.ClientInstanceID),
				ResumeLeaseID: protocol.LeaseID(saved.ResumeLeaseID),
			})
		},
	}
	return connector, store, entry
}

func remoteAdapterContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), remoteAdapterTestTimeout)
}

func TestRemoteWorkbenchTrackedRollbackCannotRemoveNewerSubscription(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-rollback-token", SessionID: "session-rollback-token"}
	firstSnapshot := remoteAdapterSnapshot(target, "runtime-rollback", "snapshot-rollback-one", 0, nil)
	secondSnapshot := remoteAdapterSnapshot(target, "runtime-rollback", "snapshot-rollback-two", 0, nil)
	unsubscribed := make(chan protocol.SubscriptionID, 2)
	var subscribes atomic.Int32
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-rollback-token", "", func(wire *rpcwire.Conn, _ net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubscribeParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				switch subscribes.Add(1) {
				case 1:
					if params.ReplaceSubscriptionID != "" {
						return nil, fmt.Errorf("first replaceSubscriptionId = %q", params.ReplaceSubscriptionID)
					}
					return protocol.SessionSubscribeResult{SubscriptionID: "subscription-rollback-one", Snapshot: firstSnapshot}, nil
				case 2:
					if params.ReplaceSubscriptionID != "subscription-rollback-one" {
						return nil, fmt.Errorf("second replaceSubscriptionId = %q", params.ReplaceSubscriptionID)
					}
					return protocol.SessionSubscribeResult{SubscriptionID: "subscription-rollback-two", Snapshot: secondSnapshot}, nil
				default:
					return nil, errors.New("unexpected extra subscribe")
				}
			})
			wire.Handle(string(protocol.MethodSessionUnsubscribe), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionUnsubscribeParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				unsubscribed <- params.SubscriptionID
				return protocol.SessionUnsubscribeResult{Unsubscribed: true}, nil
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	input := runtimeapi.AttachAndSubscribeInput{Session: mapRemoteSessionRef(target), HistoryTurns: 20}

	ctx, cancel = remoteAdapterContext(t)
	_, rollbackOld, err := adapter.attachRemoteWorkbenchSession(ctx, input)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = remoteAdapterContext(t)
	_, rollbackCurrent, err := adapter.attachRemoteWorkbenchSession(ctx, input)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = rollbackOld(ctx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-unsubscribed:
		t.Fatalf("stale rollback unsubscribed newer binding %q", id)
	case <-time.After(50 * time.Millisecond):
	}
	adapter.mu.RLock()
	current := adapter.sessions[target]
	adapter.mu.RUnlock()
	if current == nil || current.subscription != "subscription-rollback-two" {
		t.Fatalf("current subscription after stale rollback = %#v", current)
	}

	ctx, cancel = remoteAdapterContext(t)
	err = rollbackCurrent(ctx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-unsubscribed:
		if id != "subscription-rollback-two" {
			t.Fatalf("current rollback unsubscribed %q", id)
		}
	case <-time.After(remoteAdapterTestTimeout):
		t.Fatal("current tracked rollback did not unsubscribe")
	}
}

func TestRemoteWorkbenchTrackedRollbackForgetsDisconnectedRecovery(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-rollback-loss", SessionID: "session-rollback-loss"}
	snapshot := remoteAdapterSnapshot(target, "runtime-rollback-loss", "snapshot-rollback-loss", 0, nil)
	rawConnection := make(chan net.Conn, 1)
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-rollback-loss", "", func(wire *rpcwire.Conn, raw net.Conn) {
			rawConnection <- raw
			wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionSubscribeResult{SubscriptionID: "subscription-rollback-loss", Snapshot: snapshot}, nil
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	ctx, cancel = remoteAdapterContext(t)
	_, rollback, err := adapter.attachRemoteWorkbenchSession(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: mapRemoteSessionRef(target), HistoryTurns: 20,
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if err := (<-rawConnection).Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case fault := <-adapter.Faults():
		if fault == nil {
			t.Fatal("transport loss published a nil fault")
		}
	case <-time.After(remoteAdapterTestTimeout):
		t.Fatal("timed out waiting for transport loss")
	}
	if recovery := adapter.client.RecoveryState(); len(recovery.Subscriptions) != 1 || recovery.Subscriptions[0].Target != target {
		t.Fatalf("pre-rollback recovery = %#v", recovery)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = rollback(ctx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if recovery := adapter.client.RecoveryState(); len(recovery.Subscriptions) != 0 {
		t.Fatalf("rejected attach remained in reconnect recovery: %#v", recovery)
	}
	adapter.mu.RLock()
	binding := adapter.sessions[target]
	adapter.mu.RUnlock()
	if binding != nil {
		t.Fatalf("rejected attach retained adapter binding: %#v", binding)
	}
}

func TestRemoteRuntimeAdapterRealPhase5ClosedLoopAndLosslessMapping(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-opaque", SessionID: "session-opaque"}
	snapshot := remoteAdapterSnapshot(target, "runtime-opaque", "snapshot-opaque", 0, nil)
	acceptedRequestIDs := make(chan protocol.RequestID, 8)
	requestPattern := regexp.MustCompile(`^request_[0-9a-f]{64}$`)
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-one", "", func(wire *rpcwire.Conn, raw net.Conn) {
			wire.Handle(string(protocol.MethodWorkspaceBrowse), func(context.Context, json.RawMessage) (any, error) {
				return protocol.WorkspaceBrowseResult{
					Directory: protocol.DirectoryItem{DirectoryRef: "directory-root", Name: "repo", DisplayPath: "/srv/repo"},
					Entries:   []protocol.DirectoryItem{{DirectoryRef: "directory-child", Name: "src", DisplayPath: "/srv/repo/src", ParentRef: "directory-root"}},
				}, nil
			})
			wire.Handle(string(protocol.MethodWorkspaceOpen), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.WorkspaceOpenParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				acceptedRequestIDs <- params.RequestID
				return protocol.WorkspaceOpenResult{
					Workspace:   protocol.WorkspaceSummary{WorkspaceID: target.WorkspaceID, Name: "repo", DisplayPath: "/srv/repo"},
					Disposition: protocol.WorkspaceOpened,
				}, nil
			})
			wire.Handle(string(protocol.MethodSessionCreate), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionCreateParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				acceptedRequestIDs <- params.RequestID
				return protocol.SessionCreateResult{
					Target: target, RuntimeEpoch: "runtime-opaque", TopicID: "topic-opaque", TopicTitle: "Remote Topic",
					ResolvedProfile: snapshot.Meta.ResolvedProfile,
				}, nil
			})
			wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionSubscribeResult{SubscriptionID: "subscription-one", Snapshot: snapshot}, nil
			})
			wire.Handle(string(protocol.MethodSessionSubmit), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubmitParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				acceptedRequestIDs <- params.RequestID
				result := protocol.SessionSubmitResult{
					Kind: protocol.SubmitTurn, TurnID: "turn-opaque", Target: target, RuntimeEpoch: "runtime-opaque",
				}
				return rpcwire.RespondThen(result, func(writeErr error) {
					if writeErr == nil {
						go func() {
							_ = wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
								SubscriptionID: "subscription-one", HostEpoch: "host-epoch", Target: target,
								RuntimeEpoch: "runtime-opaque", Seq: 1, TurnID: "turn-opaque",
								Event: eventwire.Event{Kind: "turn_started", Text: "accepted"}, Externalized: []protocol.ExternalizedField{},
							})
						}()
					}
				}), nil
			})
			wire.Handle(string(protocol.MethodTurnSteer), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.TurnSteerParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				acceptedRequestIDs <- params.RequestID
				return protocol.TurnSteerResult{Accepted: true, TurnID: params.ExpectedTurnID}, nil
			})
			wire.Handle(string(protocol.MethodTurnCancel), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.TurnCancelParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				acceptedRequestIDs <- params.RequestID
				return protocol.TurnCancelResult{Status: protocol.CancelRequested, TurnID: params.ExpectedTurnID}, nil
			})
			wire.Handle(string(protocol.MethodPromptApprove), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.PromptApproveParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				acceptedRequestIDs <- params.RequestID
				return protocol.PromptResolvedResult{Resolved: true, PromptID: params.PromptID}, nil
			})
			wire.Handle(string(protocol.MethodPromptAnswer), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.PromptAnswerParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				acceptedRequestIDs <- params.RequestID
				return protocol.PromptResolvedResult{Resolved: true, PromptID: params.PromptID}, nil
			})
			wire.Handle(string(protocol.MethodRemoteDetach), func(context.Context, json.RawMessage) (any, error) {
				return rpcwire.RespondThen(protocol.DetachResult{Detached: true}, func(error) { _ = raw.Close() }), nil
			})
		}),
	}}
	connector, store, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	persisted, _, err := store.Get(entry.ID)
	if err != nil || persisted.ResumeLeaseID != "lease-one" {
		t.Fatalf("persisted lease = %+v, %v", persisted, err)
	}

	ctx, cancel = remoteAdapterContext(t)
	connection, err := adapter.Connection(ctx)
	cancel()
	if err != nil || connection.OS != "linux" || connection.ShellKind != "bash" || !connection.Config.Available || !connection.Capabilities.HostConfig || connection.Config.Revision != "catalog-1" {
		t.Fatalf("Connection = %+v, %v", connection, err)
	}
	ctx, cancel = remoteAdapterContext(t)
	browse, err := adapter.BrowseWorkspace(ctx, runtimeapi.BrowseWorkspaceInput{TypedPath: "/srv/repo", Limit: 20})
	cancel()
	if err != nil || browse.Directory.Ref != "directory-root" || len(browse.Entries) != 1 || browse.Entries[0].ParentRef != "directory-root" {
		t.Fatalf("BrowseWorkspace = %+v, %v", browse, err)
	}
	ctx, cancel = remoteAdapterContext(t)
	opened, err := adapter.OpenWorkspace(ctx, runtimeapi.OpenWorkspaceInput{PrimaryDirectory: "directory-root"})
	cancel()
	if err != nil || opened.Workspace.ID != runtimeapi.WorkspaceID(target.WorkspaceID) || opened.AlreadyOpen {
		t.Fatalf("OpenWorkspace = %+v, %v", opened, err)
	}
	ctx, cancel = remoteAdapterContext(t)
	created, err := adapter.CreateSession(ctx, runtimeapi.CreateSessionInput{
		WorkspaceID: runtimeapi.WorkspaceID(target.WorkspaceID), Topic: runtimeapi.TopicSelection{Kind: runtimeapi.TopicNew, Title: "Remote Topic"},
		Profile: runtimeapi.ProfileSelection{Model: "provider/model", Effort: "high", CollaborationMode: "goal", TokenMode: "economy", ToolApprovalMode: "ask"},
	})
	cancel()
	if err != nil || created.Session != mapRemoteSessionRef(target) || created.ResolvedProfile.TokenMode != "economy" {
		t.Fatalf("CreateSession = %+v, %v", created, err)
	}
	ctx, cancel = remoteAdapterContext(t)
	mapped, err := adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{Session: created.Session, HistoryTurns: 50})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Goal == nil || *mapped.Goal != "ship Remote V1" || mapped.Runtime.LastError == nil ||
		len(mapped.Runtime.LiveEvents) != 1 || len(mapped.History.Messages) != 1 ||
		mapped.History.Messages[0].ToolResultError == nil || len(mapped.Todos) != 1 ||
		mapped.Context.ElapsedMillis != 303 || len(mapped.Jobs) != 1 || len(mapped.Checkpoints) != 1 {
		t.Fatalf("lossless snapshot mapping = %+v", mapped)
	}
	ctx, cancel = remoteAdapterContext(t)
	submitted, err := adapter.ComposerSubmit(ctx, runtimeapi.ComposerSubmitInput{Session: created.Session, Input: "run tests"})
	cancel()
	if err != nil || submitted.Kind != runtimeapi.SubmitTurn || submitted.TurnID != "turn-opaque" {
		t.Fatalf("ComposerSubmit = %+v, %v", submitted, err)
	}
	select {
	case event := <-adapter.Events():
		if event.Session != created.Session || event.TurnID != "turn-opaque" || event.Value.Kind != "turn_started" {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(remoteAdapterTestTimeout):
		t.Fatal("timed out waiting for Remote event")
	}
	release, err := adapter.CanRelease(context.Background())
	if err != nil || len(release.Blockers) == 0 || release.Blockers[0].Kind != ReleaseRuntimeRunning {
		t.Fatalf("CanRelease while Turn runs = %+v, %v", release, err)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = adapter.SteerTurn(ctx, runtimeapi.SteerInput{Session: created.Session, TurnID: "turn-opaque", Text: "focus tests"})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = adapter.CancelTurn(ctx, runtimeapi.CancelTurnInput{Session: created.Session, TurnID: "turn-opaque"})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = adapter.ApprovePrompt(ctx, runtimeapi.ApproveInput{
		Session: created.Session, PromptID: "prompt-approval", Decision: runtimeapi.DecisionAllowOnce,
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = adapter.AnswerPrompt(ctx, runtimeapi.AnswerInput{
		Session: created.Session, PromptID: "prompt-ask",
		Answers: []runtimeapi.QuestionAnswer{{QuestionID: "question-1", Selected: []string{"Fresh"}}},
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	ids := make(map[protocol.RequestID]bool)
	for index := 0; index < 7; index++ {
		requestID := <-acceptedRequestIDs
		if !requestPattern.MatchString(string(requestID)) || ids[requestID] {
			t.Fatalf("requestId %q is not fresh cryptographic identity", requestID)
		}
		ids[requestID] = true
	}
	ctx, cancel = remoteAdapterContext(t)
	if err := adapter.Detach(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	persisted, _, err = store.Get(entry.ID)
	if err != nil || persisted.ResumeLeaseID != "" {
		t.Fatalf("lease after detach = %+v, %v", persisted, err)
	}
}

func TestRemoteRuntimeAdapterReconnectRestoresFreshSnapshotAndIsolatesGenerationQueues(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-reconnect", SessionID: "session-reconnect"}
	firstSnapshot := remoteAdapterSnapshot(target, "runtime-one", "snapshot-one", 0, nil)
	secondSnapshot := remoteAdapterSnapshot(target, "runtime-two", "snapshot-two", 5, remoteAdapterAskPrompt())
	firstWire := make(chan *rpcwire.Conn, 1)
	firstRaw := make(chan net.Conn, 1)
	var secondSubscribes atomic.Int32
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-one", "", func(wire *rpcwire.Conn, raw net.Conn) {
			firstWire <- wire
			firstRaw <- raw
			wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionSubscribeResult{SubscriptionID: "subscription-one", Snapshot: firstSnapshot}, nil
			})
		}),
		remoteAdapterBasePeer(buildID, "lease-two", "lease-one", func(wire *rpcwire.Conn, raw net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubscribeParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				attempt := secondSubscribes.Add(1)
				snapshot := secondSnapshot
				subscriptionID := protocol.SubscriptionID("subscription-two")
				if attempt == 1 {
					if params.ReplaceSubscriptionID != "" {
						return nil, errors.New("reconnect fresh subscribe unexpectedly replaced an old transport subscription")
					}
				} else {
					if params.ReplaceSubscriptionID != "subscription-two" {
						return nil, fmt.Errorf("explicit attach replacement = %q", params.ReplaceSubscriptionID)
					}
					subscriptionID = "subscription-three"
					snapshot.SnapshotID = "snapshot-three"
					snapshot.History.SnapshotID = snapshot.SnapshotID
					snapshot.BoundarySeq = 6
					snapshot.History.Messages[0].Content = stringPointer("current-after-event")
				}
				result := protocol.SessionSubscribeResult{SubscriptionID: subscriptionID, Snapshot: snapshot}
				return rpcwire.RespondThen(result, func(writeErr error) {
					if writeErr == nil && attempt == 1 {
						go func() {
							_ = wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
								SubscriptionID: "subscription-two", HostEpoch: "host-epoch", Target: target,
								RuntimeEpoch: "runtime-two", Seq: 6, TurnID: "turn-new",
								Event: eventwire.Event{Kind: "text", Text: "new-generation"}, Externalized: []protocol.ExternalizedField{},
							})
						}()
					}
				}), nil
			})
			wire.Handle(string(protocol.MethodRemoteDetach), func(context.Context, json.RawMessage) (any, error) {
				return rpcwire.RespondThen(protocol.DetachResult{Detached: true}, func(error) { _ = raw.Close() }), nil
			})
		}),
	}}
	connector, store, entry := newRemoteAdapterTestConnector(t, factory)
	descriptor := TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label}
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, descriptor)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{Session: mapRemoteSessionRef(target), HistoryTurns: 40})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	oldEvents := adapter.Events()
	oldFaults := adapter.Faults()
	wire := <-firstWire
	raw := <-firstRaw
	if err := wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
		SubscriptionID: "subscription-one", HostEpoch: "host-epoch", Target: target,
		RuntimeEpoch: "runtime-one", Seq: 1, TurnID: "turn-old",
		Event: eventwire.Event{Kind: "text", Text: "old-generation"}, Externalized: []protocol.ExternalizedField{},
	}); err != nil {
		t.Fatal(err)
	}
	waitRemoteAdapter(t, func() bool { return len(oldEvents) == 1 })
	_ = raw.Close()
	waitRemoteAdapter(t, func() bool {
		return adapter.client.Status().State == remoteclient.StateDisconnected && len(oldFaults) == 1
	})

	ctx, cancel = remoteAdapterContext(t)
	reconnected, err := connector.Reconnect(ctx, descriptor, adapter)
	cancel()
	if err != nil || reconnected != adapter {
		t.Fatalf("Reconnect = %T, %v", reconnected, err)
	}
	newEvents := adapter.Events()
	newFaults := adapter.Faults()
	if newEvents == oldEvents || newFaults == oldFaults {
		t.Fatal("Reconnect reused a previous connection generation stream")
	}
	select {
	case event := <-newEvents:
		if event.Value.Text != "new-generation" || event.TurnID != "turn-new" {
			t.Fatalf("new generation event = %+v", event)
		}
	case <-time.After(remoteAdapterTestTimeout):
		t.Fatal("timed out waiting for new generation event")
	}
	select {
	case event := <-oldEvents:
		if event.Value.Text != "old-generation" || event.TurnID != "turn-old" {
			t.Fatalf("old generation queue = %+v", event)
		}
	default:
		t.Fatal("old buffered event migrated into the new generation queue")
	}
	select {
	case fault := <-newFaults:
		t.Fatalf("old generation fault reached new generation: %v", fault)
	default:
	}
	select {
	case fault := <-oldFaults:
		if fault == nil {
			t.Fatal("old generation fault is nil")
		}
	default:
		t.Fatal("old generation fault left its generation queue")
	}
	ctx, cancel = remoteAdapterContext(t)
	restored, err := adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{Session: mapRemoteSessionRef(target), HistoryTurns: 40})
	cancel()
	if err != nil || restored.PendingPrompt == nil || restored.PendingPrompt.Ask == nil ||
		restored.PendingPrompt.Ask.ID != "prompt-ask" || restored.Runtime.LastError == nil ||
		len(restored.History.Messages) != 1 || restored.History.Messages[0].Content == nil ||
		*restored.History.Messages[0].Content != "current-after-event" || secondSubscribes.Load() != 2 {
		t.Fatalf("restored snapshot = %+v, subscribes=%d, err=%v", restored, secondSubscribes.Load(), err)
	}
	release, err := adapter.CanRelease(context.Background())
	if err != nil || len(release.Blockers) == 0 || release.Blockers[0].Kind != ReleasePromptPending {
		t.Fatalf("CanRelease with recovered Prompt = %+v, %v", release, err)
	}
	persisted, _, err := store.Get(entry.ID)
	if err != nil || persisted.ResumeLeaseID != "lease-two" {
		t.Fatalf("reconnected lease = %+v, %v", persisted, err)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = adapter.Detach(ctx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoteRuntimeAdapterFailedDetachPreservesResumeLease(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-preserved", "", func(wire *rpcwire.Conn, raw net.Conn) {
			wire.Handle(string(protocol.MethodRemoteDetach), func(context.Context, json.RawMessage) (any, error) {
				_ = raw.Close()
				return protocol.DetachResult{Detached: true}, nil
			})
		}),
	}}
	connector, store, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	ctx, cancel = remoteAdapterContext(t)
	err = adapter.Detach(ctx)
	cancel()
	if err == nil {
		t.Fatal("Detach unexpectedly succeeded without a response")
	}
	persisted, _, loadErr := store.Get(entry.ID)
	if loadErr != nil || persisted.ResumeLeaseID != "lease-preserved" {
		t.Fatalf("failed detach lease = %+v, %v", persisted, loadErr)
	}
}

func TestSameRemoteConnectionIdentityIncludesDirectEndpoint(t *testing.T) {
	entry, err := NewRemoteDirectHostEntry("developer@192.168.1.20", 22, "Direct")
	if err != nil {
		t.Fatal(err)
	}
	cosmetic := entry
	cosmetic.Label = "Renamed"
	cosmetic.LayoutRef = "layout_changed"
	cosmetic.ResumeLeaseID = "lease_changed"
	if !sameRemoteConnectionIdentity(entry, cosmetic) {
		t.Fatal("label, layout, and refreshed lease unexpectedly changed connection identity")
	}
	for name, mutate := range map[string]func(*RemoteHostEntry){
		"mode":        func(host *RemoteHostEntry) { host.Mode = RemoteHostConnectionConfig },
		"destination": func(host *RemoteHostEntry) { host.Destination = "developer@192.168.1.21" },
		"port":        func(host *RemoteHostEntry) { host.Port = 2222 },
		"client": func(host *RemoteHostEntry) {
			host.ClientInstanceID = strings.Replace(host.ClientInstanceID, "desktop_", "desktop_f", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := entry
			mutate(&changed)
			if sameRemoteConnectionIdentity(entry, changed) {
				t.Fatalf("%s change did not alter connection identity", name)
			}
		})
	}
}

func TestRemoteRuntimeAdapterUnknownMutationOutcomeRequiresExplicitSameRequestRetry(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-unknown", SessionID: "session-unknown"}
	snapshot := remoteAdapterSnapshot(target, "runtime-unknown", "snapshot-unknown", 0, nil)
	var submitted atomic.Int32
	requestIDs := make(chan protocol.RequestID, 2)
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-unknown", "", func(wire *rpcwire.Conn, raw net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionSubscribeResult{SubscriptionID: "subscription-unknown", Snapshot: snapshot}, nil
			})
			wire.Handle(string(protocol.MethodSessionSubmit), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubmitParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				submitted.Add(1)
				requestIDs <- params.RequestID
				_ = raw.Close() // accepted outcome is deliberately unknown to Desktop
				return protocol.SessionSubmitResult{
					Kind: protocol.SubmitTurn, TurnID: "turn-unknown", Target: target, RuntimeEpoch: "runtime-unknown",
				}, nil
			})
		}),
		remoteAdapterBasePeer(buildID, "lease-unknown", "lease-unknown", func(wire *rpcwire.Conn, _ net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionSubscribeResult{SubscriptionID: "subscription-unknown-reconnected", Snapshot: snapshot}, nil
			})
			wire.Handle(string(protocol.MethodSessionSubmit), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubmitParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				submitted.Add(1)
				requestIDs <- params.RequestID
				return protocol.SessionSubmitResult{
					Kind: protocol.SubmitTurn, TurnID: "turn-unknown", Target: target, RuntimeEpoch: "runtime-unknown",
				}, nil
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{Session: mapRemoteSessionRef(target), HistoryTurns: 20})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	var generated atomic.Int32
	adapter.newRequestID = func() (protocol.RequestID, error) {
		generated.Add(1)
		return "request_fixed_unknown_outcome", nil
	}
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.ComposerSubmit(ctx, runtimeapi.ComposerSubmitInput{Session: mapRemoteSessionRef(target), Input: "do it once"})
	cancel()
	if err == nil {
		t.Fatal("unknown mutation outcome unexpectedly succeeded")
	}
	if submitted.Load() != 1 || generated.Load() != 1 {
		t.Fatalf("unknown outcome submitted=%d requestIds=%d, want one call with one ID", submitted.Load(), generated.Load())
	}
	select {
	case requestID := <-requestIDs:
		if requestID != "request_fixed_unknown_outcome" {
			t.Fatalf("requestId = %q", requestID)
		}
	default:
		t.Fatal("Host did not accept the one mutation request")
	}

	ctx, cancel = remoteAdapterContext(t)
	if err := adapter.reconnectAndRestore(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	ctx, cancel = remoteAdapterContext(t)
	result, err := adapter.ComposerSubmit(ctx, runtimeapi.ComposerSubmitInput{Session: mapRemoteSessionRef(target), Input: "do it once"})
	cancel()
	if err != nil || result.TurnID != "turn-unknown" {
		t.Fatalf("explicit same-action retry result=%+v err=%v", result, err)
	}
	if submitted.Load() != 2 || generated.Load() != 1 {
		t.Fatalf("explicit retry submitted=%d requestIds=%d, want two sends with one generated ID", submitted.Load(), generated.Load())
	}
	select {
	case requestID := <-requestIDs:
		if requestID != "request_fixed_unknown_outcome" {
			t.Fatalf("retried requestId = %q", requestID)
		}
	default:
		t.Fatal("Host did not receive explicit retry")
	}
}

func TestRemoteRuntimeAdapterComposerAcceptsOnlyValidSessionReplacement(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	source := protocol.RuntimeTarget{WorkspaceID: "workspace-replacement", SessionID: "session-source"}
	replacement := protocol.RuntimeTarget{WorkspaceID: source.WorkspaceID, SessionID: "session-replacement"}
	snapshot := remoteAdapterSnapshot(source, "runtime-source", "snapshot-source", 0, nil)
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-replacement", "", func(wire *rpcwire.Conn, _ net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionSubscribeResult{SubscriptionID: "subscription-source", Snapshot: snapshot}, nil
			})
			wire.Handle(string(protocol.MethodSessionSubmit), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubmitParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				if params.Target != source || params.Input != "/new" {
					return nil, fmt.Errorf("unexpected replacement submit: %+v", params)
				}
				return protocol.SessionSubmitResult{
					Kind: protocol.SubmitCompleted, Effect: protocol.EffectSessionReplaced,
					Target: replacement, RuntimeEpoch: "runtime-replacement", SnapshotRequired: true,
				}, nil
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: mapRemoteSessionRef(source), HistoryTurns: 20,
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = remoteAdapterContext(t)
	result, err := adapter.ComposerSubmit(ctx, runtimeapi.ComposerSubmitInput{
		Session: mapRemoteSessionRef(source), Input: "/new",
	})
	cancel()
	if err != nil || result.Kind != runtimeapi.SubmitCompleted || result.Effect != string(runtimeapi.EffectSessionReplaced) ||
		result.Session != mapRemoteSessionRef(replacement) || !result.SnapshotRequired {
		t.Fatalf("replacement ComposerSubmit = %+v, %v", result, err)
	}
	adapter.mu.RLock()
	binding := adapter.sessions[replacement]
	adapter.mu.RUnlock()
	if binding == nil || binding.runtimeEpoch != "runtime-replacement" || binding.hasSnapshot {
		t.Fatalf("replacement binding = %+v, want epoch with mutation admission blocked until migrated snapshot", binding)
	}
}

func TestRemoteRuntimeAdapterEmitsHydratedSnapshotAfterAtomicTargetMigration(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	source := protocol.RuntimeTarget{WorkspaceID: "workspace-migrate", SessionID: "session-old"}
	replacement := protocol.RuntimeTarget{WorkspaceID: source.WorkspaceID, SessionID: "session-new"}
	sourceSnapshot := remoteAdapterSnapshot(source, "runtime-old", "snapshot-old", 4, nil)
	replacementSnapshot := remoteAdapterSnapshot(replacement, "runtime-new", "snapshot-new", 0, nil)
	wireReady := make(chan *rpcwire.Conn, 1)
	var subscribeCalls atomic.Int32
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-migrate", "", func(wire *rpcwire.Conn, _ net.Conn) {
			wireReady <- wire
			wire.Handle(string(protocol.MethodSessionSubscribe), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubscribeParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				switch subscribeCalls.Add(1) {
				case 1:
					if params.Target != source || params.ReplaceSubscriptionID != "" {
						return nil, fmt.Errorf("initial subscribe = %+v", params)
					}
					return protocol.SessionSubscribeResult{SubscriptionID: "subscription-old", Snapshot: sourceSnapshot}, nil
				case 2:
					if params.Target != replacement || params.ReplaceSubscriptionID != "subscription-old" {
						return nil, fmt.Errorf("migration subscribe = %+v", params)
					}
					if err := wire.Notify(string(protocol.MethodSessionEvent), protocol.SessionEvent{
						SubscriptionID: "subscription-new", HostEpoch: "host-epoch", Target: replacement,
						RuntimeEpoch: "runtime-new", Seq: 1,
						Event:        eventwire.Event{Kind: "text", Text: "after replacement snapshot"},
						Externalized: []protocol.ExternalizedField{},
					}); err != nil {
						return nil, err
					}
					return protocol.SessionSubscribeResult{SubscriptionID: "subscription-new", Snapshot: replacementSnapshot}, nil
				default:
					return nil, errors.New("unexpected extra subscribe")
				}
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: mapRemoteSessionRef(source), HistoryTurns: 20,
	})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	wire := <-wireReady
	if err := wire.Notify(string(protocol.MethodSessionResyncRequired), protocol.SessionResyncRequired{
		SubscriptionID: "subscription-old", HostEpoch: "host-epoch", Target: source,
		RuntimeEpoch: "runtime-old", LastSeq: 4, Reason: protocol.ResyncTargetReplaced,
		ReplacementTarget: &replacement, ReplacementRuntimeEpoch: "runtime-new",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-adapter.Events():
		if event.Session != mapRemoteSessionRef(replacement) || event.Value.Kind != "" || event.Snapshot == nil ||
			event.Snapshot.Previous != mapRemoteSessionRef(source) || event.Snapshot.Snapshot.Session != mapRemoteSessionRef(replacement) ||
			event.Snapshot.Snapshot.History.TotalTurns != replacementSnapshot.History.TotalTurns {
			t.Fatalf("snapshot migration event = %+v", event)
		}
	case <-time.After(remoteAdapterTestTimeout):
		t.Fatal("timed out waiting for replacement snapshot event")
	}
	select {
	case event := <-adapter.Events():
		if event.Snapshot != nil || event.Session != mapRemoteSessionRef(replacement) || event.Value.Kind != "text" || event.Value.Text != "after replacement snapshot" {
			t.Fatalf("post-snapshot live event = %+v", event)
		}
	case <-time.After(remoteAdapterTestTimeout):
		t.Fatal("timed out waiting for post-snapshot live event")
	}
	adapter.mu.RLock()
	_, oldRetained := adapter.sessions[source]
	current := adapter.sessions[replacement]
	adapter.mu.RUnlock()
	if oldRetained || current == nil || !current.hasSnapshot || current.subscription != "subscription-new" {
		t.Fatalf("post-migration bindings old=%v current=%+v", oldRetained, current)
	}
}

func TestRemoteRuntimeAdapterFailedMigrationPublishDropsBothAtomicReplacementBindings(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	source := protocol.RuntimeTarget{WorkspaceID: "workspace-migrate-fail", SessionID: "session-old"}
	replacement := protocol.RuntimeTarget{WorkspaceID: source.WorkspaceID, SessionID: "session-new"}
	var subscribeCalls atomic.Int32
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-migrate-fail", "", func(wire *rpcwire.Conn, _ net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubscribeParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				switch subscribeCalls.Add(1) {
				case 1:
					return protocol.SessionSubscribeResult{
						SubscriptionID: "subscription-old",
						Snapshot:       remoteAdapterSnapshot(source, "runtime-old", "snapshot-old", 1, nil),
					}, nil
				case 2:
					if params.Target != replacement || params.ReplaceSubscriptionID != "subscription-old" {
						return nil, fmt.Errorf("replacement subscribe = %+v", params)
					}
					return protocol.SessionSubscribeResult{
						SubscriptionID: "subscription-new",
						Snapshot:       remoteAdapterSnapshot(replacement, "runtime-new", "snapshot-new", 2, nil),
					}, nil
				default:
					return nil, errors.New("unexpected extra subscribe")
				}
			})
			wire.Handle(string(protocol.MethodSessionUnsubscribe), func(context.Context, json.RawMessage) (any, error) {
				return protocol.SessionUnsubscribeResult{Unsubscribed: true}, nil
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	ctx, cancel = remoteAdapterContext(t)
	if _, err := adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
		Session: mapRemoteSessionRef(source), HistoryTurns: 20,
	}); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	publishErr := errors.New("reject replacement snapshot")
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.subscribeFreshOrdered(ctx, replacement, 20, "subscription-old", source, func(runtimeapi.SessionSnapshot) error {
		return publishErr
	})
	cancel()
	if !errors.Is(err, publishErr) {
		t.Fatalf("failed migration = %v, want %v", err, publishErr)
	}
	adapter.mu.RLock()
	oldBinding := adapter.sessions[source]
	newBinding := adapter.sessions[replacement]
	adapter.mu.RUnlock()
	if oldBinding != nil || newBinding != nil {
		t.Fatalf("failed migration retained ghost bindings: old=%+v new=%+v", oldBinding, newBinding)
	}
	if recovery := adapter.client.RecoveryState(); len(recovery.Subscriptions) != 0 {
		t.Fatalf("failed migration recovery = %+v, want empty", recovery.Subscriptions)
	}
}

func TestRemoteRuntimeAdapterStaleMigrationCannotRetireNewerSourceBindingWhenTargetExists(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	source := protocol.RuntimeTarget{WorkspaceID: "workspace-stale-migrate", SessionID: "session-source"}
	replacement := protocol.RuntimeTarget{WorkspaceID: source.WorkspaceID, SessionID: "session-existing"}
	var subscribeCalls atomic.Int32
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-stale-migrate", "", func(wire *rpcwire.Conn, _ net.Conn) {
			wire.Handle(string(protocol.MethodSessionSubscribe), func(_ context.Context, payload json.RawMessage) (any, error) {
				var params protocol.SessionSubscribeParams
				if err := json.Unmarshal(payload, &params); err != nil {
					return nil, err
				}
				switch subscribeCalls.Add(1) {
				case 1:
					if params.Target != source || params.ReplaceSubscriptionID != "" {
						return nil, fmt.Errorf("first source subscribe = %+v", params)
					}
					return protocol.SessionSubscribeResult{SubscriptionID: "source-old", Snapshot: remoteAdapterSnapshot(source, "runtime-source-old", "snapshot-source-old", 1, nil)}, nil
				case 2:
					if params.Target != replacement || params.ReplaceSubscriptionID != "" {
						return nil, fmt.Errorf("existing target subscribe = %+v", params)
					}
					return protocol.SessionSubscribeResult{SubscriptionID: "target-current", Snapshot: remoteAdapterSnapshot(replacement, "runtime-target", "snapshot-target", 1, nil)}, nil
				case 3:
					if params.Target != source || params.ReplaceSubscriptionID != "source-old" {
						return nil, fmt.Errorf("newer source subscribe = %+v", params)
					}
					return protocol.SessionSubscribeResult{SubscriptionID: "source-new", Snapshot: remoteAdapterSnapshot(source, "runtime-source-new", "snapshot-source-new", 2, nil)}, nil
				default:
					return nil, errors.New("stale migration reached Host")
				}
			})
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	for _, ref := range []protocol.RuntimeTarget{source, replacement, source} {
		ctx, cancel = remoteAdapterContext(t)
		_, err = adapter.AttachAndSubscribe(ctx, runtimeapi.AttachAndSubscribeInput{
			Session: mapRemoteSessionRef(ref), HistoryTurns: 20,
		})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel = remoteAdapterContext(t)
	_, err = adapter.subscribeFreshOrdered(ctx, replacement, 20, "source-old", source, nil)
	cancel()
	if !errors.Is(err, remoteclient.ErrSubscriptionReplaced) {
		t.Fatalf("stale migration = %v, want replaced", err)
	}
	adapter.mu.RLock()
	sourceBinding := adapter.sessions[source]
	targetBinding := adapter.sessions[replacement]
	adapter.mu.RUnlock()
	if sourceBinding == nil || sourceBinding.subscription != "source-new" ||
		targetBinding == nil || targetBinding.subscription != "target-current" {
		t.Fatalf("stale migration changed bindings: source=%+v target=%+v", sourceBinding, targetBinding)
	}
	if got := subscribeCalls.Load(); got != 3 {
		t.Fatalf("subscribe calls = %d, want 3", got)
	}
	select {
	case fault := <-adapter.Faults():
		t.Fatalf("stale expected migration emitted adapter fault: %v", fault)
	default:
	}
}

func TestRemoteRuntimeAdapterProjectsCatalogInvalidationWithoutTransportIdentity(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	wireReady := make(chan *rpcwire.Conn, 1)
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-catalog", "", func(wire *rpcwire.Conn, _ net.Conn) {
			wireReady <- wire
		}),
	}}
	connector, _, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })

	wire := <-wireReady
	if err := wire.Notify(string(protocol.MethodCatalogChanged), protocol.CatalogChanged{
		HostEpoch: "host-epoch", Revision: "catalog-17", Scope: protocol.CatalogWorkspace,
		AffectedWorkspaceIDs: []protocol.WorkspaceID{"workspace-opaque"},
		Kinds:                []protocol.CatalogKind{protocol.CatalogSessions, protocol.CatalogMemory},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-adapter.CatalogEvents():
		if change.Revision != "catalog-17" || change.Scope != runtimeapi.CatalogWorkspace ||
			len(change.AffectedWorkspaceIDs) != 1 || change.AffectedWorkspaceIDs[0] != "workspace-opaque" ||
			len(change.Kinds) != 2 || change.Kinds[0] != runtimeapi.CatalogSessions || change.Kinds[1] != runtimeapi.CatalogMemory {
			t.Fatalf("Catalog invalidation = %+v", change)
		}
	case <-time.After(remoteAdapterTestTimeout):
		t.Fatal("catalog invalidation was not projected")
	}
}

func TestRemoteRuntimeAdapterCommittedDetachReportsLeasePersistenceFailure(t *testing.T) {
	buildID := remoteAdapterTestBuildID()
	factory := &remoteAdapterScriptedFactory{scripts: []remoteAdapterPeerScript{
		remoteAdapterBasePeer(buildID, "lease-will-be-stale", "", func(wire *rpcwire.Conn, raw net.Conn) {
			wire.Handle(string(protocol.MethodRemoteDetach), func(context.Context, json.RawMessage) (any, error) {
				return rpcwire.RespondThen(protocol.DetachResult{Detached: true}, func(error) { _ = raw.Close() }), nil
			})
		}),
	}}
	connector, store, entry := newRemoteAdapterTestConnector(t, factory)
	ctx, cancel := remoteAdapterContext(t)
	adapterValue, err := connector.Connect(ctx, TargetDescriptor{Kind: TargetRemote, ID: entry.ID, Label: entry.Label})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterValue.(*RemoteRuntimeAdapter)
	t.Cleanup(func() { adapter.shutdown(false) })
	if err := os.WriteFile(store.Path(), []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = remoteAdapterContext(t)
	err = adapter.Detach(ctx)
	cancel()
	var committed *RemoteDetachCommittedError
	if !errors.Is(err, ErrRemoteDetachCommitted) || !errors.As(err, &committed) ||
		committed == nil || !committed.DetachCommitted() || !errors.Is(err, ErrRemoteHostStoreCorrupt) {
		t.Fatalf("Detach error = %T %v, want committed persistence failure", err, err)
	}
	if adapter.client.Status().State != remoteclient.StateClosed {
		t.Fatalf("client status after committed detach = %+v", adapter.client.Status())
	}
	ctx, cancel = remoteAdapterContext(t)
	_, connectionErr := adapter.Connection(ctx)
	cancel()
	if connectionErr == nil {
		t.Fatal("committed detached adapter remained usable")
	}
}

func waitRemoteAdapter(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(remoteAdapterTestTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for Remote adapter condition")
}
