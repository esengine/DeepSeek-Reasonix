package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func bindInitialSessionRuntime(opts Options) (*session.Runtime, *session.ClientBinding) {
	runtime := opts.SessionRuntime
	if opts.SessionService == nil || runtime == nil {
		return runtime, nil
	}
	binding, err := opts.SessionService.Bind(runtime)
	if err != nil {
		return nil, nil
	}
	return runtime, binding
}

// releaseSessionRuntimeBinding drops this controller's client reference. The
// host service retains the writer until the last binding and activity exit.
func (c *Controller) releaseSessionRuntimeBinding(service *session.Service) {
	c.v3BindingMu.Lock()
	binding := c.sessionBinding
	runtime := c.sessionRuntime
	c.sessionBinding = nil
	c.sessionRuntime = nil
	c.v3BindingMu.Unlock()
	c.unbindExecutionControl(runtime)
	if binding != nil {
		if err := binding.Release(context.Background()); err != nil {
			slog.Warn("controller: release exclusive v3 binding", "err", err)
		}
	} else if service == nil {
		slog.Warn("controller: exclusive v3 runtime has no service binding")
	}
}

// BindFreshSession creates and publishes a fresh identity-bound session. The caller
// may provide an id allocated by its protocol; an empty id lets persistence
// allocate one. Publication happens only after the initial event batch is
// accepted, so failure leaves the currently-bound session usable.
func (c *Controller) BindFreshSession(ctx context.Context, sessionID string) (session.SessionRef, error) {
	return c.BindFreshSessionWithOptions(ctx, session.CreateOptions{SessionID: sessionID})
}

// BindFreshSessionWithOptions creates a fresh identity with immutable host
// ownership metadata before publishing the runtime.
func (c *Controller) BindFreshSessionWithOptions(ctx context.Context, options session.CreateOptions) (session.SessionRef, error) {
	service, _, _ := c.v3Binding()
	if c == nil || service == nil || c.executor == nil {
		return session.SessionRef{}, errors.New("v3 session service is unavailable")
	}
	prepared, err := service.PrepareCreate(ctx, options)
	if err != nil {
		return session.SessionRef{}, err
	}
	candidate := prepared.Runtime()
	fresh := agent.NewSession(c.basePrompt())
	if err := seedRuntimeSession(ctx, candidate, "session-create", fresh.Snapshot(), c.ModelRef(), c.ModelSelectionIdentity()); err != nil {
		_ = service.Discard(context.Background(), prepared)
		return session.SessionRef{}, err
	}
	if _, err := candidate.Session().Flush(ctx); err != nil {
		_ = service.Discard(context.Background(), prepared)
		return session.SessionRef{}, err
	}
	owner, err := service.Publish(prepared)
	if err != nil {
		_ = service.Discard(context.Background(), prepared)
		return session.SessionRef{}, err
	}
	if _, err = c.publishSessionRuntime(candidate, fresh, true); err != nil {
		// This attempt published the identity, so an owner-scoped close is the
		// correct cleanup. It still refuses while any client is bound.
		_ = owner.Close(context.Background())
		return session.SessionRef{}, err
	}
	return candidate.Ref(), nil
}

// ContinueLegacySession freezes and migrates the selected legacy head, then
// publishes the returned immutable v3 identity. The source remains only as a
// display/import locator and is never rebound as the execution store.
func (c *Controller) ContinueLegacySession(ctx context.Context, sourcePath, headID string) (session.SessionRef, error) {
	return c.continueLegacySession(ctx, sourcePath, headID, true, session.CreateOptions{})
}

// ContinueLegacySessionWithOptions installs immutable Desktop ownership in
// the same publication that materializes the imported session.
func (c *Controller) ContinueLegacySessionWithOptions(ctx context.Context, sourcePath, headID string, options session.CreateOptions) (session.SessionRef, error) {
	return c.continueLegacySession(ctx, sourcePath, headID, true, options)
}

// ContinueLegacySessionForRebuildWithOptions performs the same fail-atomic
// import while an Agent generation is being replaced for the same logical
// session, and publishes host-owned immutable metadata in that same
// transaction. The SessionTemp generation belongs to the logical session, so
// this path must not rotate it merely because persistence crossed the
// legacy/v3 boundary.
func (c *Controller) ContinueLegacySessionForRebuildWithOptions(ctx context.Context, sourcePath, headID string, options session.CreateOptions) (session.SessionRef, error) {
	return c.continueLegacySession(ctx, sourcePath, headID, false, options)
}

func (c *Controller) continueLegacySession(ctx context.Context, sourcePath, headID string, rotateSessionTemp bool, options session.CreateOptions) (session.SessionRef, error) {
	service, _, _ := c.v3Binding()
	if c == nil || service == nil || c.executor == nil {
		return session.SessionRef{}, errors.New("v3 session service is unavailable")
	}
	restoreLegacyEvents, err := c.releaseLegacyEventStoreForImport(ctx)
	if err != nil {
		return session.SessionRef{}, fmt.Errorf("freeze legacy event source: %w", err)
	}
	published := false
	defer func() {
		if !published {
			restoreLegacyEvents()
		}
	}()
	candidate, _, err := service.ContinueImportedWithHeader(ctx, sourcePath, headID, options)
	if err != nil {
		return session.SessionRef{}, err
	}
	owner, err := service.Owner(candidate)
	if err != nil {
		return session.SessionRef{}, err
	}
	if err := seedRuntimeConfig(ctx, candidate, "legacy-import-config", c.ModelRef(), c.ModelSelectionIdentity()); err != nil {
		_ = owner.Close(context.Background())
		return session.SessionRef{}, err
	}
	messages := candidate.Session().ExecutionSnapshot().Projection.ModelMessages
	prepared := agent.NewSession("").CloneWithMessages(messages)
	if _, err = c.publishSessionRuntime(candidate, prepared, rotateSessionTemp); err != nil {
		_ = owner.Close(context.Background())
		return session.SessionRef{}, err
	}
	published = true
	return candidate.Ref(), nil
}

// ContinuePrototypeSession imports the retired sidecar codec through the restricted
// fail-closed bridge, then publishes the final linear session identity.
func (c *Controller) ContinuePrototypeSession(ctx context.Context, sourceDir string) (session.SessionRef, error) {
	service, _, _ := c.v3Binding()
	if c == nil || service == nil || c.executor == nil {
		return session.SessionRef{}, errors.New("v3 session service is unavailable")
	}
	candidate, _, err := service.ContinuePrototype(ctx, sourceDir)
	if err != nil {
		return session.SessionRef{}, err
	}
	owner, err := service.Owner(candidate)
	if err != nil {
		return session.SessionRef{}, err
	}
	if err := seedRuntimeConfig(ctx, candidate, "prototype-import-config", c.ModelRef(), c.ModelSelectionIdentity()); err != nil {
		_ = owner.Close(context.Background())
		return session.SessionRef{}, err
	}
	prepared := agent.NewSession("").CloneWithMessages(candidate.Session().ExecutionSnapshot().Projection.ModelMessages)
	if _, err = c.publishSessionRuntime(candidate, prepared, true); err != nil {
		_ = owner.Close(context.Background())
		return session.SessionRef{}, err
	}
	return candidate.Ref(), nil
}

// OpenSession attaches this Controller to an existing immutable session identity.
// Opening never creates a missing session and publication retains the current
// binding until the target projection and writer are ready.
//
// Attaching grants only a ClientBinding, so a failed publication withdraws this
// client's own grant instead of disposing a runtime another client may already
// be using. A retired stored codec is the one exception: importing it publishes
// a brand-new identity that this attempt owns outright.
func (c *Controller) OpenSession(ctx context.Context, ref session.SessionRef) (session.SessionRef, error) {
	service, current, _ := c.v3Binding()
	if c == nil || service == nil || c.executor == nil {
		return session.SessionRef{}, errors.New("v3 session service is unavailable")
	}
	if current != nil && current.Ref() == ref {
		return ref, nil
	}
	binding, err := service.Open(ctx, ref)
	if errors.Is(err, session.ErrUnsupportedVersion) {
		var upgraded *session.Runtime
		upgraded, _, err = service.ContinueStoredPreview(ctx, ref.SessionID)
		if err != nil {
			return session.SessionRef{}, err
		}
		owner, ownerErr := service.Owner(upgraded)
		if ownerErr != nil {
			return session.SessionRef{}, ownerErr
		}
		return c.publishAttachedSession(upgraded, "upgrade", owner.Close)
	}
	if err != nil {
		return session.SessionRef{}, err
	}
	target := binding.Runtime()
	published, err := c.publishAttachedSession(target, "attach-existing", nil)
	if err == nil {
		// publishSessionRuntime installs the controller's own client grant; this
		// temporary attach grant is no longer needed.
		_ = binding.Release(context.Background())
		return published, nil
	}
	// Only this client's grant is withdrawn. A runtime another client still
	// holds keeps its binding count above zero and is left untouched.
	if releaseErr := binding.Release(context.Background()); releaseErr != nil {
		slog.Warn("controller: release failed v3 attach binding", "err", releaseErr)
	}
	return session.SessionRef{}, err
}

// publishAttachedSession publishes the prepared projection for an already-resolved
// runtime. retire is used only when this attempt owns a newly published
// identity; pass nil to withdraw a client grant instead.
func (c *Controller) publishAttachedSession(candidate *session.Runtime, reason string, retire func(context.Context) error) (session.SessionRef, error) {
	if candidate == nil {
		return session.SessionRef{}, errors.New("v3 session runtime is unavailable")
	}
	prepared := agent.NewSession("").CloneWithMessages(candidate.Session().ExecutionSnapshot().Projection.ModelMessages)
	if _, err := c.publishSessionRuntime(candidate, prepared, true); err != nil {
		if retire != nil {
			_ = retire(context.Background())
		}
		return session.SessionRef{}, err
	}
	return candidate.Ref(), nil
}

// SetSessionTitle records mutable title state in the canonical event stream.
func (c *Controller) SetSessionTitle(ctx context.Context, title string) error {
	_, runtime, exclusive := c.v3Binding()
	if !exclusive || runtime == nil {
		return session.ErrSessionNotRunning
	}
	payload, err := json.Marshal(map[string]string{"title": title})
	if err != nil {
		return err
	}
	snapshot := runtime.Session().ExecutionSnapshot()
	_, err = c.appendSessionBatch(ctx, runtime.Session(), session.Batch{
		OperationID: "session-title:" + agent.NewMessageID(),
		TurnID:      snapshot.Projection.TurnID,
		Events:      []session.Event{{Kind: "session/title", Payload: payload}},
	})
	return err
}

func seedRuntimeSession(ctx context.Context, runtime *session.Runtime, operationID string, messages []provider.Message, modelRef, modelIdentity string) error {
	if runtime == nil {
		return nil
	}
	events := make([]session.Event, 0, len(messages)+1)
	for _, message := range messages {
		if message.ID == "" {
			return errors.New("initial v3 message has no stable id")
		}
		payload, err := json.Marshal(map[string]any{"message": message})
		if err != nil {
			return err
		}
		events = append(events, session.Event{Kind: "message/complete", Payload: payload})
	}
	if strings.TrimSpace(modelRef) != "" {
		config, err := sessionConfigEvent(modelRef, modelIdentity)
		if err != nil {
			return err
		}
		events = append(events, config)
	}
	if len(events) == 0 {
		return nil
	}
	_, err := runtime.Session().AppendBatch(ctx, operationID, events)
	return err
}

func seedRuntimeConfig(ctx context.Context, runtime *session.Runtime, operationID, modelRef, modelIdentity string) error {
	if runtime == nil || strings.TrimSpace(modelRef) == "" {
		return nil
	}
	event, err := sessionConfigEvent(modelRef, modelIdentity)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(event.Payload)
	_, err = runtime.Session().AppendBatch(ctx, fmt.Sprintf("%s:%x", operationID, digest[:16]), []session.Event{event})
	return err
}

func sessionConfigEvent(modelRef, modelIdentity string) (session.Event, error) {
	payload, err := json.Marshal(map[string]string{"modelRef": modelRef, "modelIdentity": modelIdentity})
	if err != nil {
		return session.Event{}, err
	}
	return session.Event{Kind: "session/config", Payload: payload}, nil
}

func (c *Controller) publishSessionRuntime(candidate *session.Runtime, prepared *agent.Session, rotateSessionTemp bool) (*session.Runtime, error) {
	if candidate == nil || prepared == nil {
		return nil, errors.New("v3 runtime publication candidate is unavailable")
	}
	service, _, _ := c.v3Binding()
	if service == nil {
		return nil, errors.New("v3 session service is unavailable")
	}
	if current, ok := service.Runtime(candidate.Ref()); !ok || current != candidate {
		return nil, errors.New("v3 runtime candidate is not the exact published service instance")
	}
	projection := candidate.Session().ExecutionSnapshot().Projection
	if err := validateSessionDomainProjection(projection); err != nil {
		return nil, err
	}
	binding, err := service.Bind(candidate)
	if err != nil {
		return nil, fmt.Errorf("bind v3 runtime: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = binding.Release(context.Background())
		}
	}()
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	// Domain parsing was validated above. Restore it before swapping the
	// client binding so a future validation failure cannot expose a partially
	// published controller or require closing a shared runtime.
	if err := c.restoreSessionDomainProjection(projection); err != nil {
		return nil, err
	}
	c.mu.Lock()
	oldGen := c.turns.generation
	c.mu.Unlock()
	c.v3BindingMu.Lock()
	old := c.sessionRuntime
	oldBinding := c.sessionBinding
	c.sessionRuntime = candidate
	c.sessionBinding = binding
	c.exclusiveSession = true
	c.v3BindingMu.Unlock()
	c.bindExecutionControl()
	if old != nil && old != candidate {
		old.UnbindExecution(oldGen)
	}
	c.mu.Lock()
	// Legacy paths are import inputs only. Retaining one as the live path lets
	// unrelated compatibility helpers recreate sidecars beside a read-only
	// source. The immutable SessionRef is the sole execution identity.
	c.sessionPath = ""
	c.mu.Unlock()
	c.executor.SetSession(prepared)
	// The immutable session projection is the only Goal restore source. A true
	// session switch installs a fresh, disarmed lifecycle; OpenSession's exact-runtime
	// fast path returns before this point and therefore preserves live activation.
	c.installGoalLifecycle(candidate)
	// Transcript pages are a derived cache. A session switch invalidates the
	// prior identity immediately; the next query rebuilds from the exact v3
	// projection without reading or writing a legacy sidecar.
	c.turnEvents.mu.Lock()
	c.turnEvents.projection = nil
	c.turnEvents.projectionErr = nil
	c.turnEvents.mu.Unlock()
	c.rebindCheckpoints("")
	c.ResetPlannerSession()
	// The inbox belongs to the live runtime generation, not to the imported
	// legacy path. Close the pre-bind queue before rotating the session temp so
	// later Agent rebuilds attach to the same current generation.
	c.pauseInboxOnRotate()
	if rotateSessionTemp {
		c.rotateSessionTemp()
	}
	c.rebindInbox()
	c.refreshRuntimeState(event.Event{})
	published = true
	if oldBinding != nil && oldBinding != binding {
		if err := oldBinding.Release(context.Background()); err != nil {
			slog.Warn("controller: retire previous v3 binding after publication", "err", err)
		}
	}
	return old, nil
}

func validateSessionDomainProjection(projection session.Projection) error {
	if len(projection.PlanState) > 0 {
		var plan struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(projection.PlanState, &plan); err != nil {
			return fmt.Errorf("restore v3 plan state: %w", err)
		}
	}
	if len(projection.GoalState) > 0 {
		var goal goalState
		if err := json.Unmarshal(projection.GoalState, &goal); err != nil {
			return fmt.Errorf("restore v3 goal state: %w", err)
		}
	}
	return nil
}

func (c *Controller) restoreSessionDomainProjection(projection session.Projection) error {
	var plan struct {
		Enabled bool `json:"enabled"`
	}
	if len(projection.PlanState) > 0 {
		if err := json.Unmarshal(projection.PlanState, &plan); err != nil {
			return fmt.Errorf("restore v3 plan state: %w", err)
		}
	}
	c.mu.Lock()
	c.sessionSettings.planMode = plan.Enabled
	c.mu.Unlock()
	if setter, ok := c.runner.(interface{ SetPlanMode(bool) }); ok {
		setter.SetPlanMode(plan.Enabled)
	} else if c.executor != nil {
		c.executor.SetPlanMode(plan.Enabled)
	}
	if err := c.goals.restoreGoalEvent(projection.GoalState); err != nil {
		return fmt.Errorf("restore v3 goal state: %w", err)
	}
	if c.executor != nil {
		c.executor.RestoreDeliveryCheckpoint(c.goals.deliveryState())
	}
	return nil
}

func (c *Controller) v3Binding() (*session.Service, *session.Runtime, bool) {
	if c == nil {
		return nil, nil, false
	}
	c.v3BindingMu.RLock()
	service, runtime, exclusive := c.sessionService, c.sessionRuntime, c.exclusiveSession
	c.v3BindingMu.RUnlock()
	return service, runtime, exclusive
}

// SessionBinding exposes the host-owned service/runtime pair for an Agent
// rebuild. Callers must attach the pair to the replacement Controller; they
// must not close or republish the writer themselves.
func (c *Controller) SessionBinding() (*session.Service, *session.Runtime, bool) {
	service, runtime, exclusive := c.v3Binding()
	return service, runtime, exclusive && service != nil && runtime != nil
}

// SessionService exposes the host query/management owner without requiring
// an active runtime. Cold history listing must not create an Agent or writer.
func (c *Controller) SessionService() *session.Service {
	service, _, exclusive := c.v3Binding()
	if !exclusive {
		return nil
	}
	return service
}

// UsesExclusiveSession reports the configured execution contract even when
// a lazy fresh session has not yet been allocated. Hosts use it to avoid
// manufacturing a legacy path during rebuild preparation.
func (c *Controller) UsesExclusiveSession() bool {
	service, _, exclusive := c.v3Binding()
	return exclusive && service != nil
}

func (c *Controller) sessionEngineEnabled() bool {
	_, _, exclusive := c.v3Binding()
	return exclusive
}

type SessionRotationRequest struct {
	Source session.SessionRef
	Reason string
}

type SessionRotationPlan struct {
	CreateOptions session.CreateOptions
	Commit        func(context.Context, session.SessionRef) error
}

// rotateExclusiveSession implements /new and /clear without allocating a
// legacy transcript path. clear additionally deletes the closed source v3
// directory; new leaves it available in history.
func (c *Controller) rotateExclusiveSession(clear bool) error {
	service, runtime, _ := c.v3Binding()
	if service == nil || runtime == nil {
		return errors.New("exclusive v3 session runtime is unavailable")
	}
	oldRef := runtime.Ref()
	if err := c.Snapshot(); err != nil {
		return err
	}
	reason := "new"
	if clear {
		reason = "clear"
	}
	if err := c.extensionSessionPhase(context.Background(), extension.PointSessionRotate, dispatch.PhaseRotate, oldRef.SessionID); err != nil {
		return err
	}
	c.hooks.SessionEnd(context.Background(), reason)
	c.extensionSessionEvent(extension.PointSessionEnd, dispatch.PhaseEnd, oldRef.SessionID)
	createOptions := session.CreateOptions{}
	var commitRotation func(context.Context, session.SessionRef) error
	if c.onSessionRotation != nil {
		plan, planErr := c.onSessionRotation(context.Background(), SessionRotationRequest{Source: oldRef, Reason: reason})
		if planErr != nil {
			return planErr
		}
		createOptions, commitRotation = plan.CreateOptions, plan.Commit
	}
	ref, err := c.BindFreshSessionWithOptions(context.Background(), createOptions)
	if err != nil {
		return err
	}
	if commitRotation != nil {
		if err := commitRotation(context.Background(), ref); err != nil {
			return fmt.Errorf("new session %s is active; publish workspace membership: %w", ref.SessionID, err)
		}
	} else if clear {
		if err := service.Delete(context.Background(), oldRef); err != nil {
			return fmt.Errorf("new session %s is active; delete cleared session: %w", ref.SessionID, err)
		}
	}
	c.ClearGoal()
	c.mu.Lock()
	c.startedOnce = true
	c.mu.Unlock()
	c.hooks.SetSessionID(ref.SessionID)
	c.enqueueHookContexts(c.hooks.SessionStart(context.Background(), reason))
	c.extensionSessionEvent(extension.PointSessionStart, dispatch.PhaseStart, ref.SessionID)
	c.clearSessionWriteAccess()
	return nil
}
