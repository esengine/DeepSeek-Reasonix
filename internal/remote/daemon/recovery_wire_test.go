package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/runtimefactory"
)

type daemonRecoveryPromptRunner struct {
	controller *control.Controller
	executor   *agent.Agent

	mu      sync.Mutex
	answers []event.AskAnswer
	err     error
}

var daemonRecoveryPromptText = strings.Repeat("Choose before continuing. ", 4096)

func (r *daemonRecoveryPromptRunner) Run(ctx context.Context, input string) error {
	r.executor.Session().Add(provider.Message{Role: provider.RoleUser, Content: input})
	answers, err := r.controller.Ask(ctx, []event.AskQuestion{{
		ID: "restart-choice", Prompt: daemonRecoveryPromptText,
		Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
	}})
	r.mu.Lock()
	r.answers = append([]event.AskAnswer(nil), answers...)
	r.err = err
	r.mu.Unlock()
	return err
}

func (r *daemonRecoveryPromptRunner) result() ([]event.AskAnswer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.AskAnswer(nil), r.answers...), r.err
}

type daemonRecoveryBuilder struct {
	mu            sync.Mutex
	runner        *daemonRecoveryPromptRunner
	promptEmitted chan struct{}
	promptOnce    sync.Once
}

func newDaemonRecoveryBuilder() *daemonRecoveryBuilder {
	return &daemonRecoveryBuilder{promptEmitted: make(chan struct{})}
}

func (b *daemonRecoveryBuilder) Build(_ context.Context, options boot.Options) (control.SessionAPI, error) {
	sink := event.FuncSink(func(value event.Event) {
		options.Sink.Emit(value)
		if value.Kind == event.AskRequest {
			b.promptOnce.Do(func() { close(b.promptEmitted) })
		}
	})
	executor := agent.New(nil, nil, agent.NewSession("Remote recovery test system"), agent.Options{}, sink)
	runner := &daemonRecoveryPromptRunner{executor: executor}
	controller := control.New(control.Options{
		Runner: runner, Executor: executor, Sink: sink,
		SessionDir: options.SessionDir, WorkspaceRoot: options.WorkspaceRoot,
	})
	runner.controller = controller
	b.mu.Lock()
	b.runner = runner
	b.mu.Unlock()
	return controller, nil
}

func (b *daemonRecoveryBuilder) currentRunner() *daemonRecoveryPromptRunner {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.runner
}

func newProductionRecoveryServer(
	t *testing.T,
	hostEpoch protocol.HostEpoch,
	buildID protocol.BuildID,
	stateDir string,
	userHome string,
	builder *daemonRecoveryBuilder,
) (*Server, *catalog.Catalog) {
	t.Helper()
	catalogValue, err := catalog.New(hostEpoch, catalog.Options{
		StateDir: stateDir, UserHome: userHome,
		SessionDir:      func(string) string { return filepath.Join(userHome, ".reasonix", "sessions") },
		ProfileResolver: daemonProfileResolver{},
	})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := runtimefactory.New(runtimefactory.Options{
		Resolver: catalogValue, Builder: builder.Build,
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata := func(ctx context.Context, target protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error) {
		value, err := catalogValue.Metadata(ctx, target)
		if err != nil {
			return protocol.SessionMetaSnapshot{}, err
		}
		return protocol.SessionMetaSnapshot{
			TopicID: value.TopicID, Title: value.Title, ResolvedProfile: value.ResolvedProfile,
		}, nil
	}
	server, err := New(context.Background(), Options{
		BuildID: buildID, HostEpoch: hostEpoch,
		HostInfo:     protocol.HostInfo{OS: "linux", Arch: "amd64", ShellKind: "bash", SandboxBackend: "process"},
		Capabilities: protocol.FrozenCapabilities(false, false),
		Catalog:      catalogValue, ControllerFactory: factory, Metadata: metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, catalogValue
}

func initializeRecoveryPeer(t *testing.T, peer *daemonPeer, buildID protocol.BuildID, client protocol.ClientInstanceID) protocol.InitializeResult {
	t.Helper()
	return requestResult[protocol.InitializeResult](t, peer, protocol.MethodRemoteInitialize, protocol.InitializeParams{
		BuildID: buildID, ClientInstanceID: client,
	})
}

func subscribeRecoveryPeer(
	t *testing.T,
	peer *daemonPeer,
	hostEpoch protocol.HostEpoch,
	target protocol.RuntimeTarget,
	replace protocol.SubscriptionID,
) protocol.SessionSubscribeResult {
	t.Helper()
	return requestResult[protocol.SessionSubscribeResult](t, peer, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: hostEpoch, Target: target, PageTurns: 60, ReplaceSubscriptionID: replace,
	})
}

func TestProductionDaemonColdRecoveryWireInvalidatesPromptAndOldEpochs(t *testing.T) {
	buildID := daemonTestBuildID(t, 'd')
	stateDir := filepath.Join(t.TempDir(), "catalog")
	userHome := t.TempDir()
	workspace := t.TempDir()
	oldHost := protocol.HostEpoch("host-before-restart")
	newHost := protocol.HostEpoch("host-after-restart")

	firstBuilder := newDaemonRecoveryBuilder()
	firstServer, _ := newProductionRecoveryServer(t, oldHost, buildID, stateDir, userHome, firstBuilder)
	firstPeer := openDaemonPeer(t, firstServer, nil, nil)
	initializeRecoveryPeer(t, firstPeer, buildID, "recovery-client-old")
	browse := requestResult[protocol.WorkspaceBrowseResult](t, firstPeer, protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: oldHost, TypedPath: workspace,
	})
	opened := requestResult[protocol.WorkspaceOpenResult](t, firstPeer, protocol.MethodWorkspaceOpen, protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "recovery-open", ExpectedHostEpoch: oldHost},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	})
	created := requestResult[protocol.SessionCreateResult](t, firstPeer, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "recovery-create", ExpectedHostEpoch: oldHost},
		WorkspaceID:  opened.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{},
	})
	initial := subscribeRecoveryPeer(t, firstPeer, oldHost, created.Target, "")
	submitted := requestResult[protocol.SessionSubmitResult](t, firstPeer, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "recovery-submit", ExpectedHostEpoch: oldHost,
			Target: created.Target, ExpectedRuntimeEpoch: initial.Snapshot.RuntimeEpoch,
		},
		Input: "accepted before the Host restarted", DisplayText: "accepted before the Host restarted",
	})
	select {
	case <-firstBuilder.promptEmitted:
	case <-time.After(3 * time.Second):
		t.Fatal("production Controller did not reach its Ask prompt")
	}
	pending := subscribeRecoveryPeer(t, firstPeer, oldHost, created.Target, initial.SubscriptionID)
	if pending.Snapshot.PendingPrompt == nil || pending.Snapshot.PendingPrompt.Kind != protocol.PromptAsk || pending.Snapshot.PendingPrompt.Ask == nil {
		t.Fatalf("pre-restart pending Prompt = %+v, want Ask; runtime=%+v live=%+v", pending.Snapshot.PendingPrompt, pending.Snapshot.Runtime, pending.Snapshot.Runtime.LiveEvents)
	}
	oldPromptID := pending.Snapshot.PendingPrompt.Ask.PromptID
	oldRuntimeEpoch := pending.Snapshot.RuntimeEpoch
	if !pending.Snapshot.Runtime.Running || submitted.TurnID == "" || oldPromptID == "" {
		t.Fatalf("pre-restart runtime snapshot = %+v submit=%+v", pending.Snapshot.Runtime, submitted)
	}
	var promptDescriptor *protocol.ExternalizedField
	for index := range pending.Snapshot.Externalized {
		if pending.Snapshot.Externalized[index].JSONPointer == "/pendingPrompt/ask/questions/0/prompt" {
			promptDescriptor = &pending.Snapshot.Externalized[index]
			break
		}
	}
	if promptDescriptor == nil {
		t.Fatalf("large pending Prompt was not externalized: %+v", pending.Snapshot.Externalized)
	}
	if got := string(readRemoteContent(t, firstPeer, *promptDescriptor)); got != daemonRecoveryPromptText {
		t.Fatalf("externalized Prompt bytes changed: got=%d want=%d", len(got), len(daemonRecoveryPromptText))
	}

	// An attach EOF alone does not own the runtime. Close the peer first, then
	// perform a normal daemon shutdown while its accepted Turn is still waiting.
	firstPeer.close(t)
	firstServer.Close()
	answers, promptErr := firstBuilder.currentRunner().result()
	if !errors.Is(promptErr, context.Canceled) || len(answers) != 0 {
		t.Fatalf("Host shutdown answered the old Ask: answers=%+v err=%v", answers, promptErr)
	}

	secondBuilder := newDaemonRecoveryBuilder()
	secondServer, secondCatalog := newProductionRecoveryServer(t, newHost, buildID, stateDir, userHome, secondBuilder)
	defer secondServer.Close()
	secondPeer := openDaemonPeer(t, secondServer, nil, nil)
	initializeRecoveryPeer(t, secondPeer, buildID, "recovery-client-new")
	restored := subscribeRecoveryPeer(t, secondPeer, newHost, created.Target, "")
	if restored.Snapshot.HostEpoch != newHost || restored.Snapshot.RuntimeEpoch == oldRuntimeEpoch {
		t.Fatalf("restored identities host=%q runtime=%q oldRuntime=%q", restored.Snapshot.HostEpoch, restored.Snapshot.RuntimeEpoch, oldRuntimeEpoch)
	}
	if restored.Snapshot.PendingPrompt != nil || restored.Snapshot.Runtime.Running || restored.Snapshot.Runtime.CurrentTurn != nil {
		t.Fatalf("cold restart restored executable state: pending=%+v runtime=%+v", restored.Snapshot.PendingPrompt, restored.Snapshot.Runtime)
	}
	interruption := restored.Snapshot.Runtime.Interruption
	if restored.Snapshot.Runtime.LastOutcome != protocol.OutcomeInterrupted || interruption == nil ||
		!interruption.PreviousTurnInterrupted || interruption.Reason != protocol.InterruptionHostRestarted {
		t.Fatalf("cold restart interruption = %+v", restored.Snapshot.Runtime)
	}

	staleHost := requestError(t, secondPeer, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "recovery-old-host-cancel", ExpectedHostEpoch: oldHost,
			Target: created.Target, ExpectedRuntimeEpoch: oldRuntimeEpoch,
		},
		ExpectedTurnID: submitted.TurnID,
	})
	requireRemoteError(t, staleHost, protocol.ErrStaleHostEpoch)
	staleRuntime := requestError(t, secondPeer, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "recovery-old-runtime-cancel", ExpectedHostEpoch: newHost,
			Target: created.Target, ExpectedRuntimeEpoch: oldRuntimeEpoch,
		},
		ExpectedTurnID: submitted.TurnID,
	})
	requireRemoteError(t, staleRuntime, protocol.ErrStaleRuntimeEpoch)
	stalePrompt := requestError(t, secondPeer, protocol.MethodPromptAnswer, protocol.PromptAnswerParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "recovery-old-prompt-answer", ExpectedHostEpoch: newHost,
			Target: created.Target, ExpectedRuntimeEpoch: restored.Snapshot.RuntimeEpoch,
		},
		PromptID: oldPromptID, Answers: []protocol.QuestionAnswer{{QuestionID: "restart-choice", Selected: []string{"A"}}},
	})
	requireRemoteError(t, stalePrompt, protocol.ErrPromptNotPending)
	resolved, err := secondCatalog.ResolveRuntimeTarget(context.Background(), created.Target)
	if err != nil {
		t.Fatal(err)
	}
	session, err := resolved.LoadSession()
	if err != nil {
		t.Fatal(err)
	}
	messages := session.Snapshot()
	if len(messages) == 0 || messages[len(messages)-1].Role != provider.RoleUser ||
		!strings.Contains(messages[len(messages)-1].Content, "accepted before the Host restarted") {
		t.Fatalf("cold-repaired transcript = %+v, want accepted real user prompt retained", messages)
	}
	meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.InFlightTurn != nil {
		t.Fatalf("cold resume did not consume in-flight marker: %+v", meta.InFlightTurn)
	}
}

var _ host.RecoveryControllerFactory = (*runtimefactory.Factory)(nil)
