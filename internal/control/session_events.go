package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/turnevent"
)

func sessionDirectory(sessionPath string) string {
	sessionPath = filepath.Clean(strings.TrimSpace(sessionPath))
	if sessionPath == "." || sessionPath == "" {
		return ""
	}
	// Windows spellings of one path must retain one shared writer identity.
	if runtime.GOOS == "windows" {
		sessionPath = strings.ToLower(sessionPath)
	}
	parent := filepath.Dir(sessionPath)
	root := filepath.Join(parent, "sessions-v4")
	if filepath.Base(parent) == "sessions" {
		root = filepath.Join(filepath.Dir(parent), "sessions-v4")
	}
	id := agent.BranchID(sessionPath)
	if id == "" {
		return ""
	}
	return filepath.Join(root, id)
}

type sharedSessionEventStore struct {
	store *session.Session
	refs  int
}

var processSessionEventStores = struct {
	sync.Mutex
	stores map[string]*sharedSessionEventStore
}{stores: map[string]*sharedSessionEventStore{}}

func acquireSessionEventStore(dir, id string) (*session.Session, func(context.Context) error, bool, error) {
	processSessionEventStores.Lock()
	defer processSessionEventStores.Unlock()
	if entry := processSessionEventStores.stores[dir]; entry != nil {
		entry.refs++
		return entry.store, releaseSessionEventStore(dir, entry), true, nil
	}
	store, err := session.Open(dir, id)
	if errors.Is(err, session.ErrSessionNotFound) {
		store, err = session.CreateStore(dir, id)
	}
	if err != nil {
		return nil, nil, false, err
	}
	entry := &sharedSessionEventStore{store: store, refs: 1}
	processSessionEventStores.stores[dir] = entry
	return store, releaseSessionEventStore(dir, entry), false, nil
}

func releaseSessionEventStore(dir string, entry *sharedSessionEventStore) func(context.Context) error {
	var once sync.Once
	var releaseErr error
	return func(ctx context.Context) error {
		once.Do(func() {
			// Every ownership handoff is a semantic checkpoint even when another
			// in-process controller already retains the physical writer.
			_, releaseErr = entry.store.Flush(ctx)
			processSessionEventStores.Lock()
			current := processSessionEventStores.stores[dir]
			if current != entry {
				processSessionEventStores.Unlock()
				return
			}
			entry.refs--
			last := entry.refs == 0
			if last {
				delete(processSessionEventStores.stores, dir)
			}
			processSessionEventStores.Unlock()
			if last {
				releaseErr = errors.Join(releaseErr, entry.store.Close(ctx))
			}
		})
		return releaseErr
	}
}

func (c *Controller) openSessionEventStore(sessionPath string) (*session.Session, func(context.Context) error, error) {
	if service, runtime, exclusive := c.v3Binding(); runtime != nil {
		return runtime.Session(), nil, nil
	} else if exclusive && service != nil {
		// A service-backed controller needs a published Runtime before admission.
		// Creating a path-derived sidecar here would reintroduce a second producer
		// outside the v3.1 ownership boundary.
		return nil, nil, nil
	}
	dir := sessionDirectory(sessionPath)
	if dir == "" {
		return nil, nil, nil
	}
	id := agent.BranchID(sessionPath)
	store, release, shared, err := acquireSessionEventStore(dir, id)
	if err != nil {
		return nil, nil, err
	}
	if !shared {
		if _, _, err := store.RecoverInterrupted(context.Background()); err != nil {
			_ = release(context.Background())
			return nil, nil, err
		}
	}
	snapshot := store.StateSnapshot()
	if snapshot.EventSequence != 0 {
		return store, release, nil
	}
	return store, release, nil
}

// releaseLegacyEventStoreForImport stops this controller's retired path-bound
// event producer before the importer takes a shared freeze lock. Other
// controllers keep their own reference; in that case the freeze correctly
// refuses to race an active producer instead of copying a moving prefix.
func (c *Controller) releaseLegacyEventStoreForImport(ctx context.Context) (func(), error) {
	if c == nil {
		return func() {}, nil
	}
	path := c.SessionPath()
	c.turnEvents.mu.Lock()
	if c.turnEvents.v3 == nil || strings.HasPrefix(c.turnEvents.v3Path, "session:") {
		c.turnEvents.mu.Unlock()
		return func() {}, nil
	}
	store := c.turnEvents.v3
	release := c.turnEvents.v3Release
	c.turnEvents.v3 = nil
	c.turnEvents.v3Path = ""
	c.turnEvents.v3Runtime = nil
	c.turnEvents.v3Release = nil
	c.turnEvents.mu.Unlock()
	var err error
	if release != nil {
		err = release(ctx)
	} else {
		err = store.Close(ctx)
	}
	restore := func() {
		if _, runtime, _ := c.v3Binding(); runtime == nil {
			c.rebindTurnEvents(path)
		}
	}
	return restore, err
}

// SuspendLegacyEventStoreForImport closes this controller's path-derived
// compatibility producer while an idle host prepares a replacement runtime.
// The caller must serialize turn admission for the controller. Calling the
// returned function restores the producer when candidate preparation fails;
// after a successful publication the old controller is retired instead.
func (c *Controller) SuspendLegacyEventStoreForImport(ctx context.Context) (func(), error) {
	return c.releaseLegacyEventStoreForImport(ctx)
}

func (c *Controller) seedSessionEventsFromExecutor(reason string) error {
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	if snapshot, ok := c.sessionEventSnapshot(); !ok || snapshot.EventSequence != 0 {
		return nil
	}
	if c == nil || c.executor == nil {
		return nil
	}
	// Existing legacy transcripts are loaded by Resume after Controller
	// construction. Seeding the constructor's system-only placeholder here
	// would make it look authoritative and discard the loaded history.
	if reason == "session-open" {
		if info, err := os.Stat(c.SessionPath()); err == nil && info.Size() > 0 {
			return nil
		}
	}
	messages := c.executor.Session().Snapshot()
	for i := range messages {
		if strings.TrimSpace(messages[i].ID) == "" {
			messages[i].ID = agent.NewMessageID()
		}
	}
	if err := c.RecordSessionMessages(context.Background(), reason, messages); err != nil {
		return err
	}
	return nil
}

// importLegacyResumeOverPlaceholder repairs the only safe pre-migration
// overlap: an older build may have created a system-only v3 sidecar before it
// loaded an existing legacy transcript. No executed turn can be present in
// that projection, so replacing it with the frozen loaded history is lossless.
func (c *Controller) importLegacyResumeOverPlaceholder(incoming *agent.Session) error {
	if c == nil || incoming == nil {
		return nil
	}
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	snapshot, ok := c.sessionEventSnapshot()
	if !ok || len(snapshot.Projection.ModelMessages) != 1 || len(incoming.Snapshot()) <= 1 {
		return nil
	}
	messages := incoming.Snapshot()
	for i := range messages {
		if strings.TrimSpace(messages[i].ID) == "" {
			messages[i].ID = agent.NewMessageID()
		}
	}
	return c.replaceSessionEventProjection(context.Background(), "legacy-resume-placeholder", messages)
}

func (c *Controller) restoreExecutorFromSessionEvents() {
	if c == nil || c.executor == nil {
		return
	}
	snapshot, ok := c.sessionEventSnapshot()
	if !ok || len(snapshot.Projection.ModelMessages) == 0 {
		return
	}
	if reflect.DeepEqual(c.executor.Session().Snapshot(), snapshot.Projection.ModelMessages) {
		return
	}
	// Called only at construction or explicit Resume. Path rewrites and recovery
	// rebinds must keep their prepared candidate session instead of adopting the
	// source log again.
	c.executor.Session().Replace(append([]provider.Message(nil), snapshot.Projection.ModelMessages...))
}

// replaceSessionModelContext records an Agent/configuration rebuild without
// rewriting the UI transcript. The exact serialized model context becomes a
// typed event, while the original messages remain available for history.
func (c *Controller) replaceSessionModelContext(ctx context.Context, messages []provider.Message, reason string) error {
	store := c.sessionEventStore()
	if store == nil {
		return errors.New("exclusive v3 controller has no session store")
	}
	snapshot := store.ExecutionSnapshot()
	modelMessages := provider.ModelMessages(messages)
	if modelMessages == nil {
		modelMessages = []provider.Message{}
	}
	payload, err := json.Marshal(map[string]any{
		"messages":        modelMessages,
		"reason":          reason,
		"sourceSequences": []uint64{snapshot.EventSequence},
	})
	if err != nil {
		return err
	}
	events := []session.Event{{Kind: "model/context-replace", Payload: payload}}
	if strings.TrimSpace(c.ModelRef()) != "" {
		configPayload, err := json.Marshal(map[string]string{"modelRef": c.ModelRef(), "modelIdentity": c.ModelSelectionIdentity()})
		if err != nil {
			return err
		}
		events = append(events, session.Event{Kind: "session/config", Payload: configPayload})
	}
	digest := sha256.Sum256(payload)
	c.turnEvents.commitMu.Lock()
	batch := session.Batch{
		OperationID: fmt.Sprintf("model-context:%x", digest[:16]),
		TurnID:      snapshot.Projection.TurnID,
		Events:      events,
	}
	_, runtime, exclusive := c.v3Binding()
	if exclusive && runtime != nil && runtime.Session() == store && !runtime.OwnsExecution(c.ExecutionGeneration()) {
		prepared, prepareErr := store.PrepareBatchContext(ctx, batch.OperationID, batch)
		if prepareErr == nil {
			if previous := c.turnEvents.pendingExecutionCommit; previous != nil {
				previous.Release()
			}
			c.turnEvents.pendingExecutionCommit = &prepared
		}
		err = prepareErr
	} else {
		_, err = c.appendSessionBatch(ctx, store, batch)
	}
	c.turnEvents.commitMu.Unlock()
	if err != nil {
		return err
	}
	if c.executor != nil {
		c.executor.Session().Replace(append([]provider.Message(nil), modelMessages...))
	}
	return nil
}

func (c *Controller) adoptResumeSystemPrompt(incoming *agent.Session) error {
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	store := c.sessionEventStore()
	if store == nil || incoming == nil {
		return nil
	}
	snapshot := store.ExecutionSnapshot()
	persisted, current := snapshot.Projection.ModelMessages, incoming.Snapshot()
	if len(persisted) == 0 || len(current) == 0 || persisted[0].Role != provider.RoleSystem || current[0].Role != provider.RoleSystem || reflect.DeepEqual(persisted[0], current[0]) {
		return nil
	}
	replaced := append([]provider.Message(nil), persisted...)
	replaced[0] = current[0]
	payload, err := json.Marshal(map[string]any{"messages": replaced, "reason": "system-prompt-refresh"})
	if err != nil {
		return err
	}
	c.turnEvents.commitMu.Lock()
	defer c.turnEvents.commitMu.Unlock()
	_, err = c.appendSessionBatch(context.Background(), store, session.Batch{
		OperationID: fmt.Sprintf("system-prompt-refresh:%d", snapshot.EventSequence+1),
		TurnID:      snapshot.Projection.TurnID,
		Events:      []session.Event{{Kind: "history/replace", Payload: payload}},
	})
	return err
}

func (c *Controller) sessionEventStore() *session.Session {
	if c == nil {
		return nil
	}
	c.turnEvents.mu.RLock()
	defer c.turnEvents.mu.RUnlock()
	return c.turnEvents.v3
}

// sessionEventCommitAllowed fences unpublished Desktop replacement runtimes.
// Those candidates intentionally share the active process store so they can
// inspect the latest in-memory prefix, but they do not own mutation authority
// until the tab's final lease handoff succeeds.
func (c *Controller) sessionEventCommitAllowed() bool {
	if _, runtime, _ := c.v3Binding(); runtime != nil {
		return runtime.OwnsExecution(c.ExecutionGeneration())
	}
	if c == nil || !c.managedSessionEvents.Load() {
		return true
	}
	if c.executor == nil || c.executor.Session() == nil {
		return false
	}
	path := c.SessionPath()
	if path == "" {
		return true
	}
	auth := c.executor.Session().WriteAuthority()
	return auth != nil && auth.Covers(path)
}

func (c *Controller) sessionEventSnapshot() (session.Snapshot, bool) {
	store := c.sessionEventStore()
	if store == nil {
		return session.Snapshot{}, false
	}
	return store.ExecutionSnapshot(), true
}

func (c *Controller) sessionStateSnapshot() (session.Snapshot, bool) {
	store := c.sessionEventStore()
	if store == nil {
		return session.Snapshot{}, false
	}
	return store.StateSnapshot(), true
}

// appendSessionEventLocked mirrors the existing event.Sink lifecycle into the
// single typed business log. turnEvents.commitMu must be held, which preserves
// the exact order assigned by the compatibility envelope adapter.
func (c *Controller) appendSessionEventLocked(ctx context.Context, e event.Event) error {
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	store := c.sessionEventStore()
	if store == nil {
		if c.sessionEngineEnabled() {
			return session.ErrSessionNotRunning
		}
		return nil
	}
	snapshot := store.ExecutionSnapshot()
	projection := snapshot.Projection
	if projection.Recovery != nil && projection.Recovery.State == "recovery_required" && e.Kind != event.TurnDone {
		// Recovery has sealed business-state mutation. The watchdog observes
		// the uncooperative worker; late semantic output must never reactivate
		// tools, interactions, Goal, or Todo.
		return nil
	}
	if e.TurnID == "" {
		if _, turnID, active := c.currentTurnToken(); active {
			e.TurnID = turnID
		}
		if e.TurnID == "" {
			if ledger := c.turnEventLedger(); ledger != nil {
				e.TurnID = ledger.ActiveTurnID()
			}
		}
		if e.TurnID == "" {
			e.TurnID = projection.TurnID
		}
	}
	events, err := c.sessionEventsFor(e, projection)
	if err != nil || len(events) == 0 {
		return err
	}
	if e.Kind == event.TurnDone {
		if err := c.appendTerminationLocked(ctx, e, store, events); err != nil {
			return fmt.Errorf("%w: %w", turnevent.ErrTurnLedgerUnavailable, err)
		}
		return nil
	}
	op := fmt.Sprintf("runtime:%s:%d:%d", e.TurnID, snapshot.EventSequence+1, e.Kind)
	_, err = c.appendSessionBatch(ctx, store, session.Batch{OperationID: op, TurnID: e.TurnID, Events: events})
	if err != nil {
		return fmt.Errorf("%w: %w", turnevent.ErrTurnLedgerUnavailable, err)
	}
	c.noteCommittedMessagesLocked(events)
	return nil
}

func (c *Controller) sessionEventsFor(e event.Event, projection session.Projection) ([]session.Event, error) {
	if e.Kind == event.Notice {
		return mcpDisplayNoticeEvents(e)
	}
	return c.v3EventsFor(e, projection)
}

func (c *Controller) v3EventsFor(e event.Event, projection session.Projection) ([]session.Event, error) {
	makePayload := func(value any) (json.RawMessage, error) { return json.Marshal(value) }
	var out []session.Event
	switch e.Kind {
	case event.TurnStarted:
		payload, err := makePayload(map[string]any{"status": event.TurnInProgress})
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "turn/start", Payload: payload})
		out = c.appendSubmissionEvent(out, e.TurnID)
		if e.DomainKind != "" {
			if e.DomainKind != "goal/state" || len(e.DomainPayload) == 0 {
				return nil, fmt.Errorf("unsupported turn admission domain event %q", e.DomainKind)
			}
			out = append(out, session.Event{Kind: e.DomainKind, Payload: append(json.RawMessage(nil), e.DomainPayload...)})
		}
	case event.ToolDispatch:
		payload, err := makePayload(v3ToolPayload(e.Tool, false))
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "tool/call", Payload: payload})
	case event.ToolStarted:
		payload, err := makePayload(map[string]any{"id": e.Tool.ID, "name": e.Tool.Name})
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "tool/start", Payload: payload})
	case event.ToolResult:
		// Tool results are emitted before Agent mutates its derived conversation
		// cache. Commit the exact message and structured result together so a
		// recovered prefix cannot contain only one half.
		if e.CommittedMessage != nil {
			messagePayload, err := makePayload(map[string]any{"message": *e.CommittedMessage})
			if err != nil {
				return nil, err
			}
			out = append(out, session.Event{Kind: "message/complete", Payload: messagePayload})
		}
		payload, err := makePayload(v3ToolPayload(e.Tool, true))
		if err != nil {
			return nil, err
		}
		if e.Tool.TodoWritten {
			todoPayload, err := makePayload(map[string]any{"todos": e.Tool.Todos})
			if err != nil {
				return nil, err
			}
			out = append(out, session.Event{Kind: "todo/write", Payload: todoPayload})
		}
		out = append(out, session.Event{Kind: "tool/result", Payload: payload})
	case event.StreamAttempt:
		payload, err := makePayload(map[string]any{
			"id": e.StreamAttempt.ID, "messageId": e.MessageID,
			"action": e.StreamAttempt.Action, "attempt": e.StreamAttempt.Attempt,
			"max": e.StreamAttempt.Max, "reason": e.StreamAttempt.Reason,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "assistant/attempt", Payload: payload})
	case event.AskRequest, event.ApprovalRequest, event.MCPInteractionRequest:
		kind := strings.TrimSpace(e.PromptKind)
		if kind == "" {
			switch e.Kind {
			case event.AskRequest:
				kind = "ask"
			case event.MCPInteractionRequest:
				kind = "mcp"
			default:
				kind = "approval"
			}
		}
		payload, err := makePayload(map[string]any{
			"id": e.ItemID, "toolCallId": e.ItemID, "kind": kind, "state": "pending",
			"sessionId": e.SessionID, "headId": agent.BranchID(c.SessionPath()),
			"turnId": e.TurnID, "runtimeEpoch": e.RuntimeEpoch,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "interaction/created", Payload: payload})
	case event.PromptAnswered:
		state := strings.TrimSpace(e.InteractionState)
		if state == "" {
			state = "answered"
		}
		payload, err := makePayload(map[string]any{"id": e.ItemID, "state": state})
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "interaction/resolved", Payload: payload})
		if e.DomainKind != "" {
			out = append(out, session.Event{Kind: e.DomainKind, Payload: append(json.RawMessage(nil), e.DomainPayload...)})
		}
	case event.TurnStatusChanged:
		if e.Status == event.TurnRecoveryRequired && e.Recovery != nil {
			payload, err := makePayload(e.Recovery)
			if err != nil {
				return nil, err
			}
			out = append(out, session.Event{Kind: "runtime/recovery", Payload: payload})
		}
	case event.CompactionDone:
		// Context-maintenance commits persist their exact model projection before
		// this notification is emitted. CompactionDone is presentation-only.
	case event.TurnDone:
		interactionState := "unavailable"
		if e.Cancelled || e.Status == event.TurnInterrupted {
			interactionState = "cancelled"
		}
		out = append(out, session.ClosureEvents(projection, interactionState, "turn ended before recording a result")...)
		if e.Recovery != nil && e.Recovery.State == "recovery_required" {
			recoveryPayload, marshalErr := makePayload(e.Recovery)
			if marshalErr != nil {
				return nil, marshalErr
			}
			out = append(out, session.Event{Kind: "runtime/recovery", Payload: recoveryPayload})
		}
		payload, err := makePayload(map[string]any{"status": terminalTurnStatus(e)})
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "turn/end", Payload: payload})
	}
	return out, nil
}

// v3ToolPayload retains the structured execution and presentation metadata
// emitted by the tool runtime. UI truncation may choose a smaller view later;
// the authoritative event must not collapse a result to name plus text.
func v3ToolPayload(tool event.Tool, result bool) map[string]any {
	payload := map[string]any{
		"id": tool.ID, "name": tool.Name, "args": tool.Args,
		"runState": tool.RunState, "diagnostic": tool.Diagnostic,
		"resolvedName": tool.ResolvedName, "capabilityId": tool.CapabilityID,
		"readOnly": tool.ReadOnly, "truncated": tool.Truncated,
		"durationMs": tool.DurationMs, "startedAt": tool.StartedAt, "endedAt": tool.EndedAt,
		"partial": tool.Partial, "argChars": tool.ArgChars, "refreshed": tool.Refreshed,
		"parentId": tool.ParentID, "attemptId": tool.AttemptID,
		"subagentRef": tool.SubagentRef, "subagentStatus": tool.SubagentStatus,
		"subagentErrorCode": tool.SubagentErrorCode, "subagentRetryable": tool.SubagentRetryable,
		"diff": tool.Diff, "added": tool.Added, "removed": tool.Removed,
		"profile": tool.Profile, "execution": tool.Execution,
		"presentedFiles":    tool.PresentedFiles,
		"workspaceMutation": tool.WorkspaceMutation, "workspacePaths": tool.WorkspacePaths,
		"workspaceAllPaths": tool.WorkspaceAllPaths,
	}
	if result {
		payload["output"] = tool.Output
		payload["error"] = tool.Err
		payload["todos"] = tool.Todos
		payload["todoWritten"] = tool.TodoWritten
	}
	return payload
}

// RecordSessionMessages implements agent.SessionEventRecorder. Runtime message
// commits arrive here directly; this code never diffs the mutable legacy
// transcript to infer missing business events.
func (c *Controller) RecordSessionMessages(ctx context.Context, reason string, messages []provider.Message) error {
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	store := c.sessionEventStore()
	if store == nil || len(messages) == 0 {
		return nil
	}
	if recovery := store.ExecutionSnapshot().Projection.Recovery; recovery != nil && recovery.State == "recovery_required" {
		return nil
	}
	c.turnEvents.commitMu.Lock()
	defer c.turnEvents.commitMu.Unlock()
	if !c.messageCommitAllowedLocked(ctx, store) {
		return nil
	}
	events := make([]session.Event, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.ID) == "" {
			return errors.New("record session message: missing stable message id")
		}
		payload, err := json.Marshal(map[string]any{"message": message})
		if err != nil {
			return err
		}
		events = append(events, session.Event{Kind: "message/complete", Payload: payload})
	}
	snapshot := store.ExecutionSnapshot()
	op := fmt.Sprintf("messages:%s:%d", reason, snapshot.EventSequence+1)
	_, err := c.appendSessionBatch(ctx, store, session.Batch{OperationID: op, TurnID: snapshot.Projection.TurnID, Events: events})
	if err == nil {
		c.noteCommittedMessagesLocked(events)
	}
	return err
}

// RecordSessionModelContext implements agent.SessionModelContextRecorder. It
// persists the exact provider-visible projection without mutating the Agent or
// the canonical history. A successful return means the accepted commit is on
// stable storage through its final sequence.
func (c *Controller) RecordSessionModelContext(ctx context.Context, request agent.SessionModelContextCommit) (agent.SessionModelContextCommitResult, error) {
	var result agent.SessionModelContextCommitResult
	if !c.sessionEventCommitAllowed() {
		return result, session.ErrStaleExecution
	}
	store := c.sessionEventStore()
	if store == nil {
		return result, nil
	}
	operationID := strings.TrimSpace(request.OperationID)
	if operationID == "" {
		return result, errors.New("record session model context: missing operation id")
	}
	modelMessages := provider.ModelMessages(request.Messages)
	if modelMessages == nil {
		modelMessages = []provider.Message{}
	}
	payload, err := json.Marshal(map[string]any{
		"messages": modelMessages,
		"reason":   strings.TrimSpace(request.Reason),
	})
	if err != nil {
		return result, err
	}

	c.turnEvents.commitMu.Lock()
	if !c.messageCommitAllowedLocked(ctx, store) {
		c.turnEvents.commitMu.Unlock()
		return result, session.ErrStaleExecution
	}
	commit, err := c.appendSessionBatch(ctx, store, session.Batch{
		OperationID: "model-context-maintenance:" + operationID,
		Events:      []session.Event{{Kind: "model/context-replace", Payload: payload}},
	})
	c.turnEvents.commitMu.Unlock()
	if err != nil {
		return result, err
	}
	result.Accepted = true
	receipt, err := store.Flush(ctx)
	if err != nil {
		return result, err
	}
	if receipt.DurableSequence < commit.LastSequence() {
		return result, fmt.Errorf("record session model context: durable sequence %d is before commit %d", receipt.DurableSequence, commit.LastSequence())
	}
	result.Durable = true
	return result, nil
}

// RecordSessionMessageUpsert records one explicit stable-message mutation.
// This is used for local metadata such as protocol recovery receipts, whose
// provider-visible bytes may not change but whose durable UI/business fact must
// survive without falling back to a full legacy transcript rewrite.
func (c *Controller) RecordSessionMessageUpsert(ctx context.Context, reason string, message provider.Message) error {
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	store := c.sessionEventStore()
	if store == nil {
		return nil
	}
	if strings.TrimSpace(message.ID) == "" {
		return errors.New("record session message upsert: missing stable message id")
	}
	payload, err := json.Marshal(map[string]any{"message": message})
	if err != nil {
		return err
	}
	c.turnEvents.commitMu.Lock()
	defer c.turnEvents.commitMu.Unlock()
	if !c.messageCommitAllowedLocked(ctx, store) {
		return nil
	}
	snapshot := store.ExecutionSnapshot()
	op := fmt.Sprintf("message-upsert:%s:%s:%d", reason, message.ID, snapshot.EventSequence+1)
	_, err = c.appendSessionBatch(ctx, store, session.Batch{OperationID: op, TurnID: snapshot.Projection.TurnID, Events: []session.Event{{Kind: "message/upsert", Payload: payload}}})
	return err
}

// replaceSessionEventProjection records an intentional transcript rewrite as
// an explicit context event. Recovery, cancellation and host prompt changes use
// this path so the typed log remains authoritative without diffing the legacy
// message cache at a later checkpoint.
func (c *Controller) replaceSessionEventProjection(ctx context.Context, reason string, messages []provider.Message) error {
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	store := c.sessionEventStore()
	if store == nil {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"messages": append([]provider.Message(nil), messages...),
		"reason":   reason,
	})
	if err != nil {
		return err
	}
	c.turnEvents.commitMu.Lock()
	defer c.turnEvents.commitMu.Unlock()
	snapshot := store.ExecutionSnapshot()
	op := fmt.Sprintf("context-replace:%s:%d", reason, snapshot.EventSequence+1)
	_, err = c.appendSessionBatch(ctx, store, session.Batch{
		OperationID: op,
		TurnID:      snapshot.Projection.TurnID,
		Events:      []session.Event{{Kind: "history/replace", Payload: payload}},
	})
	return err
}

func (c *Controller) flushSessionEvents(ctx context.Context) (session.DurableReceipt, error) {
	store := c.sessionEventStore()
	if store == nil {
		return session.DurableReceipt{}, nil
	}
	return store.Flush(ctx)
}

func (c *Controller) appendDomainState(kind string, payload json.RawMessage, reason string) error {
	if !c.sessionEventCommitAllowed() {
		return nil
	}
	store := c.sessionEventStore()
	if store == nil || len(payload) == 0 {
		return nil
	}
	if recovery := store.ExecutionSnapshot().Projection.Recovery; recovery != nil && recovery.State == "recovery_required" {
		return nil
	}
	c.turnEvents.commitMu.Lock()
	defer c.turnEvents.commitMu.Unlock()
	if !c.messageCommitAllowedLocked(context.Background(), store) {
		return nil
	}
	snapshot := store.ExecutionSnapshot()
	op := fmt.Sprintf("domain:%s:%s:%d", kind, reason, snapshot.EventSequence+1)
	_, err := c.appendSessionBatch(context.Background(), store, session.Batch{OperationID: op, TurnID: snapshot.Projection.TurnID, Events: []session.Event{{Kind: kind, Payload: append(json.RawMessage(nil), payload...)}}})
	if err != nil {
		return fmt.Errorf("%w: %w", turnevent.ErrTurnLedgerUnavailable, err)
	}
	return nil
}
