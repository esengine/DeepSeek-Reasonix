package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// durableAdmission owns one already-committed Remote user anchor. The runtime
// host prepares it synchronously, then the next guarded body claims it. done is
// intentionally the body-start boundary, not the Turn-completion boundary: the
// latter is reported by the ordinary TurnDone event after final persistence.
type durableAdmission struct {
	mu      sync.Mutex
	claimed bool
	done    chan struct{}
	result  DurableTurnAdmissionResult
	once    sync.Once

	prepareDone  chan struct{}
	session      *agent.Session
	messageIndex int
	input        DurableTurnInput
}

func newDurableAdmission(input DurableTurnInput) *durableAdmission {
	return &durableAdmission{
		done:        make(chan struct{}),
		prepareDone: make(chan struct{}),
		input:       input,
	}
}

func (a *durableAdmission) claim() {
	a.mu.Lock()
	a.claimed = true
	a.mu.Unlock()
}

func (a *durableAdmission) wasClaimed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.claimed
}

func (a *durableAdmission) resolve(result DurableTurnAdmissionResult) {
	a.once.Do(func() {
		a.mu.Lock()
		result.Claimed = a.claimed
		a.result = result
		a.mu.Unlock()
		close(a.done)
	})
}

func (a *durableAdmission) wait() DurableTurnAdmissionResult {
	<-a.done
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.result
}

// durableTurnExecution remains on the top-level context for every internal
// plan/goal/subagent model turn. firstStart is consumed exactly once so only
// the visible outer turn uses the preaccepted message boundary; synthetic
// continuations use their own current message count while still knowing that
// the top-level marker is already owned by the wrapper.
type durableTurnExecution struct {
	admission *durableAdmission
	mu        sync.Mutex
	started   bool
}

type durableTurnExecutionContextKey struct{}

func durableExecutionFromContext(ctx context.Context) *durableTurnExecution {
	value, _ := ctx.Value(durableTurnExecutionContextKey{}).(*durableTurnExecution)
	return value
}

func preparedDurableTurn(ctx context.Context) bool {
	return durableExecutionFromContext(ctx) != nil
}

func preparedTurnStart(ctx context.Context, fallback int) (int, bool) {
	execution := durableExecutionFromContext(ctx)
	if execution == nil || execution.admission == nil {
		return fallback, false
	}
	execution.mu.Lock()
	defer execution.mu.Unlock()
	if execution.started {
		return fallback, false
	}
	execution.started = true
	return execution.admission.messageIndex, true
}

func joinTurnErrors(primary, persistence error) error {
	if persistence == nil {
		return primary
	}
	if primary == nil {
		return persistence
	}
	return errors.Join(primary, persistence)
}

// EnableDurableTurnAdmission changes only this Controller's admission policy.
// Runtime hosts call it while the Controller is idle after Resume. Local
// frontends never enable it and preserve their existing asynchronous lifecycle.
func (c *Controller) EnableDurableTurnAdmission() error {
	if c == nil || c.executor == nil {
		return errors.New("durable Turn admission requires an executor")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running || c.finishing || c.rotating || c.operation != nil {
		return ErrTurnRunning
	}
	c.durableTurnAdmission = true
	// The old Agent-level hook ran after classifiers/Coordinator work and was
	// therefore too late to be a semantic admission boundary. Prepared turns use
	// agent.WithAcceptedTurn instead; explicitly remove a stale hook when a
	// rebuilt Controller reuses an Agent in tests or an embedding.
	c.executor.SetAcceptedTurnPersistHook(nil)
	return nil
}

// PrepareDurableTurn commits one stable user-visible anchor before the host
// invokes Submit*. No refs, hooks, classifier, Goal/AutoResearch mutation,
// checkpoint, Coordinator, provider, or tool work can run before this method
// succeeds. DisplayText is preferred because it is the text the user actually
// saw; the execution layer later reuses this exact message slot for the fully
// composed provider input.
func (c *Controller) PrepareDurableTurn(input DurableTurnInput) (func() DurableTurnAdmissionResult, error) {
	if c == nil || c.executor == nil {
		return nil, errors.New("durable Turn admission requires an executor")
	}
	stable := input.DisplayText
	if strings.TrimSpace(stable) == "" {
		stable = input.Input
	}
	if strings.TrimSpace(stable) == "" {
		return nil, errors.New("durable Turn admission requires non-empty user input")
	}

	admission := newDurableAdmission(input)
	c.mu.Lock()
	switch {
	case !c.durableTurnAdmission:
		c.mu.Unlock()
		return nil, errors.New("durable Turn admission is not enabled")
	case c.durableAdmissionPoison != nil:
		err := c.durableAdmissionPoison
		c.mu.Unlock()
		return nil, fmt.Errorf("durable Turn admission is poisoned: %w", err)
	case c.closed:
		c.mu.Unlock()
		return nil, errors.New("Controller is closed")
	case c.running || c.finishing || c.rotating || c.operation != nil:
		c.mu.Unlock()
		return nil, ErrTurnRunning
	case c.durableAdmissionPreparing != nil || c.durableAdmissionPending != nil || c.durableAdmissionActive != nil:
		c.mu.Unlock()
		return nil, errors.New("durable Turn admission is already prepared")
	}
	c.durableAdmissionPreparing = admission
	c.mu.Unlock()

	finishPreparation := func(publish bool) {
		c.mu.Lock()
		if c.durableAdmissionPreparing == admission {
			c.durableAdmissionPreparing = nil
		}
		if publish {
			c.durableAdmissionPending = admission
		}
		c.mu.Unlock()
		close(admission.prepareDone)
	}
	failPreparation := func(err error, before []provider.Message, session *agent.Session) (func() DurableTurnAdmissionResult, error) {
		if c.executor != nil && c.executor.Session() == session {
			session.Replace(before)
		}
		if clearErr := c.clearInFlightTurn(); clearErr != nil {
			err = errors.Join(err, fmt.Errorf("clear failed durable Turn marker: %w", clearErr))
			c.poisonDurableAdmission(err)
		}
		finishPreparation(false)
		return nil, err
	}

	session := c.executor.Session()
	if session == nil || !session.HasSystemMessage() {
		finishPreparation(false)
		return nil, errors.New("prepare durable Turn: session has no leading system message")
	}
	before := session.Snapshot()
	messageIndex := len(before)
	admission.session = session
	admission.messageIndex = messageIndex
	if err := c.markInFlightTurn(messageIndex, true); err != nil {
		if clearErr := c.clearInFlightTurn(); clearErr != nil {
			err = errors.Join(err, fmt.Errorf("clear failed durable Turn marker: %w", clearErr))
			c.poisonDurableAdmission(err)
		}
		finishPreparation(false)
		return nil, err
	}
	message := provider.Message{Role: provider.RoleUser, Content: stable}
	if input.EditedOriginal != "" {
		message.Edited = true
		message.Original = input.EditedOriginal
	}
	session.Add(message)
	commit := c.snapshotWithCommit(false, false)
	if !commit.TranscriptCommitted {
		err := commit.Err
		if err == nil {
			err = errors.New("prepare durable Turn: snapshot did not commit the transcript")
		}
		return failPreparation(err, before, session)
	}
	if commit.Err != nil {
		// The append-only transcript is authoritative. Listing/revision sidecars
		// are repairable accelerators and must not make a committed request
		// retryable (which would duplicate the user message).
		slog.Warn("controller: durable Turn transcript committed; metadata repair deferred", "err", commit.Err)
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
			Text: "session transcript was saved; metadata repair will be retried automatically"})
	}
	finishPreparation(true)

	return func() DurableTurnAdmissionResult {
		// runGuarded claims synchronously before Submit* returns. If a primitive
		// panicked or unexpectedly did not start a body, the durable user is still
		// an accepted Turn: finalize/clear it synchronously and let the Host cache
		// the TurnID plus a failed TurnDone rather than making it retryable.
		if !admission.wasClaimed() {
			c.mu.Lock()
			if c.durableAdmissionPending == admission {
				c.durableAdmissionPending = nil
			}
			shutdown := c.runtimeShutdown || c.closed
			c.mu.Unlock()
			err := errors.New("Controller did not start the prepared durable Turn")
			if !shutdown {
				err = joinTurnErrors(err, c.finalizePreparedDurableTurn(admission))
			} else {
				c.poisonDurableAdmission(err)
			}
			admission.resolve(DurableTurnAdmissionResult{SemanticCommit: true, Err: err})
		}
		return admission.wait()
	}, nil
}

func (c *Controller) poisonDurableAdmission(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.durableAdmissionPoison == nil {
		c.durableAdmissionPoison = err
	}
	c.mu.Unlock()
}

func (c *Controller) recordPreparedDurableDisplay(admission *durableAdmission) {
	if admission == nil || strings.TrimSpace(admission.input.DisplayText) == "" || c.executor == nil {
		return
	}
	messages := c.executor.Session().Snapshot()
	index := admission.messageIndex
	if index < 0 || index >= len(messages) || messages[index].Role != provider.RoleUser {
		return
	}
	c.recordDisplay(messages[index].Content, admission.input.DisplayText)
}

// finalizePreparedDurableTurn is the only successful owner of a prepared
// marker. The display mapping is recorded before the final transcript snapshot;
// then the marker is cleared. Either persistence failure poisons this Controller
// so another Prepare cannot overwrite the only crash-recovery proof.
func (c *Controller) finalizePreparedDurableTurn(admission *durableAdmission) error {
	c.recordPreparedDurableDisplay(admission)
	commit := c.snapshotWithCommit(true, false)
	if !commit.TranscriptCommitted {
		err := commit.Err
		if err == nil {
			err = errors.New("snapshot did not commit")
		}
		err = fmt.Errorf("persist final durable Turn transcript: %w", err)
		c.poisonDurableAdmission(err)
		return err
	}
	if commit.Err != nil {
		// As at initial admission, the append-only transcript is authoritative.
		// A listing/revision sidecar failure is repairable and must not retain an
		// otherwise completed crash marker.
		slog.Warn("controller: final durable Turn transcript committed; metadata repair deferred", "err", commit.Err)
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
			Text: "session transcript was saved; metadata repair will be retried automatically"})
	}
	if err := c.clearInFlightTurn(); err != nil {
		err = fmt.Errorf("clear final durable Turn marker: %w", err)
		c.poisonDurableAdmission(err)
		return err
	}
	return nil
}

// claimDurableAdmissionLocked binds the prepared anchor to exactly one guarded
// top-level body. c.mu must be held. The wrapper injects Agent's one-shot
// accepted-turn reference, resolves Host admission at body start, and owns final
// snapshot/marker cleanup after every internal plan/goal/subagent turn returns.
func (c *Controller) claimDurableAdmissionLocked(body func(context.Context) error) func(context.Context) error {
	admission := c.durableAdmissionPending
	if admission == nil {
		return body
	}
	c.durableAdmissionPending = nil
	admission.claim()
	return func(ctx context.Context) (err error) {
		c.mu.Lock()
		if c.durableAdmissionActive != nil {
			c.mu.Unlock()
			err = errors.New("another durable Turn admission is active")
			c.poisonDurableAdmission(err)
			admission.resolve(DurableTurnAdmissionResult{SemanticCommit: true, Err: err})
			return err
		}
		c.durableAdmissionActive = admission
		c.mu.Unlock()

		execution := &durableTurnExecution{admission: admission}
		ctx = context.WithValue(ctx, durableTurnExecutionContextKey{}, execution)
		ctx = agent.WithAcceptedTurn(ctx, admission.session, admission.messageIndex)
		if !agent.OnAcceptedTurnReuse(ctx, func(content string) {
			// Register before the Agent/Coordinator can rewrite the stable anchor.
			// The callback runs synchronously before that rewrite becomes visible to
			// autosave, closing the SIGKILL window where composed/compiler text was
			// durable but its user-facing display mapping was not.
			c.recordDisplay(content, admission.input.DisplayText)
		}) {
			err = errors.New("register durable Turn display observer")
			c.poisonDurableAdmission(err)
			admission.resolve(DurableTurnAdmissionResult{SemanticCommit: true, Err: err})
			return err
		}
		admission.resolve(DurableTurnAdmissionResult{SemanticCommit: true})

		defer func() {
			c.mu.Lock()
			if c.durableAdmissionActive == admission {
				c.durableAdmissionActive = nil
			}
			c.mu.Unlock()
			if recovered := recover(); recovered != nil {
				panicErr := fmt.Errorf("durable Turn panicked: %v", recovered)
				c.poisonDurableAdmission(panicErr)
				panic(recovered)
			}
		}()

		err = body(ctx)
		c.mu.Lock()
		preserve := c.runtimeShutdown && err != nil
		c.mu.Unlock()
		if preserve {
			c.poisonDurableAdmission(err)
			return err
		}
		return joinTurnErrors(err, c.finalizePreparedDurableTurn(admission))
	}
}
