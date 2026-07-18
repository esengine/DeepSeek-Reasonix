package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
)

var (
	ErrLocalTargetSuspended = errors.New("Local runtime is suspended while another target is selected")
	ErrLocalTargetBuilding  = errors.New("Local runtime is still building")
	ErrLocalSessionUnknown  = errors.New("Local RuntimeAPI session is not open")
	ErrLocalTurnMismatch    = errors.New("Local RuntimeAPI turn ID does not match the active turn")
)

// appLocalTargetState is the process-wide Local runtime admission gate. The
// zero value is deliberately admitted: Local-only desktop usage and existing
// tests do not need to construct a TargetManager to keep working.
type appLocalTargetState struct {
	lifecycleMu sync.Mutex
	admissionMu sync.RWMutex

	suspended  bool
	resuming   bool
	generation uint64

	// controllerBuildSealed is the lock-free build boundary used by all
	// controller construction paths, including a synchronous workspace repair
	// that may already hold an admission read lock. activeControllerBuilds lets
	// suspension refuse, rather than overlap, a build already in progress.
	controllerBuildSealed  atomic.Bool
	activeControllerBuilds atomic.Int64
	resumingBuilds         atomic.Bool

	// resumeBuildHook is a deterministic test seam. Production always uses the
	// normal tab controller build path below.
	resumeBuildHook func(context.Context, *App, *WorkspaceTab) error
}

func (a *App) beginLocalControllerBuild() bool {
	if a == nil {
		return false
	}
	if a.localTarget.controllerBuildSealed.Load() && !a.localTarget.resumingBuilds.Load() {
		return false
	}
	a.localTarget.activeControllerBuilds.Add(1)
	if a.localTarget.controllerBuildSealed.Load() && !a.localTarget.resumingBuilds.Load() {
		a.localTarget.activeControllerBuilds.Add(-1)
		return false
	}
	return true
}

func (a *App) endLocalControllerBuild() {
	if a != nil {
		a.localTarget.activeControllerBuilds.Add(-1)
	}
}

func (a *App) beginLocalTargetAdmission() error {
	if a == nil {
		return ErrLocalTargetSuspended
	}
	a.localTarget.admissionMu.RLock()
	if a.localTarget.suspended {
		a.localTarget.admissionMu.RUnlock()
		return ErrLocalTargetSuspended
	}
	return nil
}

func (a *App) endLocalTargetAdmission() {
	if a != nil {
		a.localTarget.admissionMu.RUnlock()
	}
}

func (a *App) cancelLocalTab(tabID string) error {
	if err := a.beginLocalTargetAdmission(); err != nil {
		return err
	}
	defer a.endLocalTargetAdmission()
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.Cancel()
	}
	return nil
}

func (a *App) approveLocalPrompt(tabID, id string, allow, session, persist bool) error {
	if err := a.beginLocalTargetAdmission(); err != nil {
		return err
	}
	defer a.endLocalTargetAdmission()
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return a.workspaceNotReadyErr(a.tabByID(tabID))
	}
	ctrl.Approve(id, allow, session, persist)
	return nil
}

func (a *App) answerLocalPrompt(tabID, id string, answers []QuestionAnswer) error {
	if err := a.beginLocalTargetAdmission(); err != nil {
		return err
	}
	defer a.endLocalTargetAdmission()
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return a.workspaceNotReadyErr(a.tabByID(tabID))
	}
	out := make([]event.AskAnswer, len(answers))
	for i, answer := range answers {
		out[i] = event.AskAnswer{QuestionID: answer.QuestionID, Selected: append([]string(nil), answer.Selected...)}
	}
	ctrl.AnswerQuestion(id, out)
	return nil
}

func (a *App) localReleaseStatusAtBarrier() ReleaseStatus {
	a.mu.RLock()
	tabs := a.runtimeTabsLocked()
	sort.Slice(tabs, func(i, j int) bool {
		if tabs[i] == nil {
			return false
		}
		if tabs[j] == nil {
			return true
		}
		return tabs[i].ID < tabs[j].ID
	})
	blockers := make([]ReleaseBlocker, 0)
	for _, tab := range tabs {
		if tab == nil || tab.Ctrl == nil {
			continue
		}
		status := tab.Ctrl.RuntimeStatus()
		if status.Running {
			blockers = append(blockers, ReleaseBlocker{Kind: ReleaseRuntimeRunning, Detail: "tab " + tab.ID + " has a running turn"})
		}
		if status.PendingPrompt {
			blockers = append(blockers, ReleaseBlocker{Kind: ReleasePromptPending, Detail: "tab " + tab.ID + " has a pending prompt"})
		}
		if status.BackgroundJobs > 0 {
			blockers = append(blockers, ReleaseBlocker{Kind: ReleaseRuntimeRunning, Detail: fmt.Sprintf("tab %s has %d background job(s)", tab.ID, status.BackgroundJobs)})
		}
	}
	a.mu.RUnlock()
	return ReleaseStatus{Blockers: blockers}
}

// localCanRelease takes the admission write barrier while it observes every
// controller. A submit cannot begin halfway through this check. Detach repeats
// the same check after closing admission, because TargetManager intentionally
// leaves the Local target usable between preflight and physical switching.
func (a *App) localCanRelease(ctx context.Context) (ReleaseStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ReleaseStatus{}, ctx.Err()
	default:
	}
	a.localTarget.admissionMu.Lock()
	defer a.localTarget.admissionMu.Unlock()
	if a.localTarget.suspended {
		return ReleaseStatus{}, nil
	}
	return a.localReleaseStatusAtBarrier(), nil
}

type localSuspendItem struct {
	tab     *WorkspaceTab
	ctrl    control.SessionAPI
	hostKey string
}

func (a *App) suspendLocalTarget(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.localTarget.lifecycleMu.Lock()
	defer a.localTarget.lifecycleMu.Unlock()
	a.localTarget.admissionMu.Lock()
	defer a.localTarget.admissionMu.Unlock()

	if a.localTarget.suspended {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if status := a.localReleaseStatusAtBarrier(); status.Busy() {
		return &TargetBusyError{Status: status}
	}
	a.localTarget.controllerBuildSealed.Store(true)
	buildSealCommitted := false
	defer func() {
		if !buildSealCommitted {
			a.localTarget.controllerBuildSealed.Store(false)
		}
	}()
	if a.localTarget.activeControllerBuilds.Load() != 0 {
		return ErrLocalTargetBuilding
	}

	// Controller rebuilds do not enter the turn gate. Refuse a transition while
	// one is still booting, then serialize the snapshot/close stretch against
	// settings-driven synchronous rebuilds.
	a.mu.RLock()
	for _, tab := range a.runtimeTabsLocked() {
		if tab != nil && tab.buildCancel != nil {
			a.mu.RUnlock()
			return ErrLocalTargetBuilding
		}
	}
	a.mu.RUnlock()
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	if status := a.localReleaseStatusAtBarrier(); status.Busy() {
		return &TargetBusyError{Status: status}
	}

	a.mu.RLock()
	tabs := a.runtimeTabsLocked()
	items := make([]localSuspendItem, 0, len(tabs))
	for _, tab := range tabs {
		if tab != nil && tab.Ctrl != nil {
			items = append(items, localSuspendItem{tab: tab, ctrl: tab.Ctrl})
		}
	}
	a.mu.RUnlock()
	for _, item := range items {
		if err := item.ctrl.Snapshot(); err != nil {
			return fmt.Errorf("snapshot Local tab %s before target switch: %w", item.tab.ID, err)
		}
	}

	a.mu.Lock()
	for index := range items {
		item := &items[index]
		if item.tab.Ctrl != item.ctrl {
			a.mu.Unlock()
			return errors.New("Local controller changed during target suspension")
		}
	}
	// Admission remains write-locked until every in-process controller and
	// lease is gone. Therefore Remote can never overlap a late Local submit.
	a.localTarget.suspended = true
	a.localTarget.resuming = false
	a.localTarget.generation++
	for index := range items {
		item := &items[index]
		item.tab.Ctrl = nil
		item.tab.Ready = false
		item.tab.ActivityStatus = ""
		item.hostKey = takeTabSharedHostKey(item.tab)
	}
	// Detached runtimes are not layout. Their durable Sessions remain on disk,
	// while clearing this process-local cache prevents a hidden Local runtime
	// from coexisting with Remote.
	a.detachedSessions = map[string]*WorkspaceTab{}
	a.saveTabsLocked()
	a.mu.Unlock()
	buildSealCommitted = true

	for _, item := range items {
		a.quiesceTabAutosave(item.tab)
		item.ctrl.Close()
		if item.hostKey != "" {
			a.releaseSharedHost(item.hostKey)
		}
		item.tab.releaseSessionLease()
	}
	return nil
}

func (a *App) resumeLocalTarget(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.localTarget.lifecycleMu.Lock()
	defer a.localTarget.lifecycleMu.Unlock()

	a.localTarget.admissionMu.Lock()
	if !a.localTarget.suspended {
		a.localTarget.admissionMu.Unlock()
		return nil
	}
	a.localTarget.resuming = true
	a.localTarget.generation++
	a.localTarget.resumingBuilds.Store(true)
	resumeCommitted := false
	defer func() {
		if !resumeCommitted {
			a.localTarget.resumingBuilds.Store(false)
		}
	}()
	resumeGeneration := a.localTarget.generation
	hook := a.localTarget.resumeBuildHook
	a.localTarget.admissionMu.Unlock()

	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			tabs = append(tabs, tab)
		}
	}
	a.mu.RUnlock()
	if len(tabs) == 0 {
		a.localTarget.admissionMu.Lock()
		a.localTarget.resuming = false
		a.localTarget.resumingBuilds.Store(false)
		a.localTarget.admissionMu.Unlock()
		return errors.New("cannot resume Local target without a workspace tab")
	}

	for _, tab := range tabs {
		select {
		case <-ctx.Done():
			a.abortLocalResume(tabs, resumeGeneration)
			return ctx.Err()
		default:
		}
		if hook != nil {
			if err := hook(ctx, a, tab); err != nil {
				a.abortLocalResume(tabs, resumeGeneration)
				return fmt.Errorf("resume Local tab %s: %w", tab.ID, err)
			}
			continue
		}
		a.startTabControllerBuild(tab)
	}
	if hook == nil {
		if err := a.waitForLocalResume(ctx, tabs); err != nil {
			a.abortLocalResume(tabs, resumeGeneration)
			return err
		}
	}

	a.mu.RLock()
	validationErr := error(nil)
	for _, tab := range tabs {
		if a.tabs[tab.ID] != tab || !tab.Ready || tab.Ctrl == nil || tab.StartupErr != "" {
			validationErr = fmt.Errorf("Local tab %s did not finish building", tab.ID)
			break
		}
	}
	a.mu.RUnlock()
	if validationErr != nil {
		a.abortLocalResume(tabs, resumeGeneration)
		return validationErr
	}
	a.localTarget.admissionMu.Lock()
	if !a.localTarget.suspended || !a.localTarget.resuming || a.localTarget.generation != resumeGeneration {
		a.localTarget.admissionMu.Unlock()
		a.abortLocalResume(tabs, resumeGeneration)
		return errors.New("Local target resume was superseded")
	}
	a.localTarget.suspended = false
	a.localTarget.resuming = false
	a.localTarget.generation++
	a.localTarget.resumingBuilds.Store(false)
	a.localTarget.controllerBuildSealed.Store(false)
	resumeCommitted = true
	a.localTarget.admissionMu.Unlock()
	return nil
}

func (a *App) waitForLocalResume(ctx context.Context, tabs []*WorkspaceTab) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		allDone := true
		a.mu.RLock()
		for _, tab := range tabs {
			if a.tabs[tab.ID] != tab {
				a.mu.RUnlock()
				return fmt.Errorf("Local tab %s disappeared while resuming", tab.ID)
			}
			if tab.StartupErr != "" && tab.buildCancel == nil {
				err := fmt.Errorf("Local tab %s failed to build: %s", tab.ID, tab.StartupErr)
				a.mu.RUnlock()
				return err
			}
			if !tab.Ready || tab.Ctrl == nil || tab.buildCancel != nil {
				allDone = false
			}
		}
		a.mu.RUnlock()
		if allDone {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *App) abortLocalResume(tabs []*WorkspaceTab, generation uint64) {
	items := make([]localSuspendItem, 0, len(tabs))
	a.mu.Lock()
	for _, tab := range tabs {
		if a.tabs[tab.ID] != tab {
			continue
		}
		a.supersedeTabBuildLocked(tab)
		if tab.Ctrl != nil {
			items = append(items, localSuspendItem{tab: tab, ctrl: tab.Ctrl, hostKey: takeTabSharedHostKey(tab)})
			tab.Ctrl = nil
		}
		tab.Ready = false
	}
	a.mu.Unlock()
	for _, item := range items {
		item.ctrl.Close()
		if item.hostKey != "" {
			a.releaseSharedHost(item.hostKey)
		}
		item.tab.releaseSessionLease()
	}
	for a.localTarget.activeControllerBuilds.Load() != 0 {
		time.Sleep(5 * time.Millisecond)
	}
	a.localTarget.admissionMu.Lock()
	if a.localTarget.generation == generation {
		a.localTarget.resuming = false
		a.localTarget.resumingBuilds.Store(false)
	}
	a.localTarget.admissionMu.Unlock()
}

// LocalTargetAdapter projects the existing in-process App/Controller runtime
// through RuntimeAPI. It contains identity and projection glue only; all
// business operations are delegated to the existing Local implementation.
type LocalTargetAdapter struct {
	app        *App
	descriptor TargetDescriptor
	v1         localRuntimeV1State

	mu          sync.Mutex
	closed      bool
	sessions    map[runtimeapi.SessionRef]*localRuntimeSession
	tabSessions map[string]runtimeapi.SessionRef
	prompts     map[runtimeapi.PromptID]localPromptBinding
	rawPrompts  map[string]runtimeapi.PromptID
	queue       []runtimeapi.Event
	wake        chan struct{}
	stop        chan struct{}
	events      chan runtimeapi.Event
	done        chan struct{}
}

type localRuntimeSession struct {
	tabID            string
	ref              runtimeapi.SessionRef
	currentTurn      runtimeapi.TurnID
	currentOperation *localRuntimeOperation
	cancelRequested  bool
	pendingPrompt    *runtimeapi.PendingPrompt
	liveEvents       []eventwire.Event
	subscribed       bool
}

type localPromptBinding struct {
	tabID       string
	rawPromptID string
	questions   map[runtimeapi.QuestionID]string
}

func NewLocalTargetAdapter(app *App) (*LocalTargetAdapter, error) {
	if app == nil {
		return nil, errors.New("Local target App is required")
	}
	app.localTarget.admissionMu.RLock()
	suspended := app.localTarget.suspended
	app.localTarget.admissionMu.RUnlock()
	if suspended {
		return nil, ErrLocalTargetSuspended
	}
	adapter := &LocalTargetAdapter{
		app:         app,
		descriptor:  TargetDescriptor{Kind: TargetLocal, ID: "local", Label: "This computer"},
		v1:          newLocalRuntimeV1State(),
		sessions:    make(map[runtimeapi.SessionRef]*localRuntimeSession),
		tabSessions: make(map[string]runtimeapi.SessionRef),
		prompts:     make(map[runtimeapi.PromptID]localPromptBinding),
		rawPrompts:  make(map[string]runtimeapi.PromptID),
		wake:        make(chan struct{}, 1),
		stop:        make(chan struct{}),
		events:      make(chan runtimeapi.Event),
		done:        make(chan struct{}),
	}
	app.localRuntimeAdapterMu.Lock()
	if app.localRuntimeAdapter != nil {
		app.localRuntimeAdapterMu.Unlock()
		return nil, errors.New("a Local RuntimeAPI adapter is already registered")
	}
	app.localRuntimeAdapter = adapter
	app.localRuntimeAdapterMu.Unlock()
	go adapter.dispatch()
	return adapter, nil
}

func ResumeLocalTargetAdapter(ctx context.Context, app *App) (*LocalTargetAdapter, error) {
	if app == nil {
		return nil, errors.New("Local target App is required")
	}
	if err := app.resumeLocalTarget(ctx); err != nil {
		return nil, err
	}
	return NewLocalTargetAdapter(app)
}

func (a *LocalTargetAdapter) Descriptor() TargetDescriptor      { return a.descriptor }
func (a *LocalTargetAdapter) RuntimeAPI() runtimeapi.RuntimeAPI { return a }

// AbandonTarget stops only the RuntimeAPI observer. App.shutdown remains the
// owner of any Local controllers when a process exit races active work.
func (a *LocalTargetAdapter) AbandonTarget() error {
	a.closeAdapter()
	return nil
}

func (a *LocalTargetAdapter) CanRelease(ctx context.Context) (ReleaseStatus, error) {
	return a.app.localCanRelease(ctx)
}

func (a *LocalTargetAdapter) Detach(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	if err := a.app.suspendLocalTarget(ctx); err != nil {
		return err
	}
	a.closeAdapter()
	return nil
}

func (a *LocalTargetAdapter) closeAdapter() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	a.v1.close()
	close(a.stop)
	a.mu.Unlock()
	a.app.localRuntimeAdapterMu.Lock()
	if a.app.localRuntimeAdapter == a {
		a.app.localRuntimeAdapter = nil
	}
	a.app.localRuntimeAdapterMu.Unlock()
	<-a.done
}

func (a *App) publishLocalRuntimeEvent(tabID string, value event.Event) {
	if a == nil {
		return
	}
	a.localRuntimeAdapterMu.RLock()
	adapter := a.localRuntimeAdapter
	a.localRuntimeAdapterMu.RUnlock()
	if adapter != nil {
		adapter.publish(tabID, value)
	}
}

func (a *LocalTargetAdapter) dispatch() {
	defer close(a.done)
	defer close(a.events)
	for {
		select {
		case <-a.stop:
			return
		case <-a.wake:
		}
		for {
			a.mu.Lock()
			if len(a.queue) == 0 {
				a.mu.Unlock()
				break
			}
			value := a.queue[0]
			a.queue[0] = runtimeapi.Event{}
			a.queue = a.queue[1:]
			a.mu.Unlock()
			select {
			case <-a.stop:
				return
			case a.events <- value:
			}
		}
	}
}

func (a *LocalTargetAdapter) Events() <-chan runtimeapi.Event { return a.events }

func (a *LocalTargetAdapter) publish(tabID string, value event.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.refreshSessionsLocked()
	ref, ok := a.tabSessions[tabID]
	if !ok {
		return
	}
	state := a.sessions[ref]
	if state == nil {
		return
	}
	wire := eventwire.ToWire(value)
	if wire.Kind == "turn_started" {
		if state.currentTurn == "" {
			id, err := newLocalOpaqueID("local_turn")
			if err != nil {
				return
			}
			state.currentTurn = runtimeapi.TurnID(id)
		}
		state.cancelRequested = false
		state.liveEvents = []eventwire.Event{}
	}
	turnID := state.currentTurn
	operationID := runtimeapi.OperationID("")
	if state.currentOperation != nil {
		operationID = state.currentOperation.id
	}

	switch wire.Kind {
	case "approval_request":
		if wire.Approval != nil {
			promptID, binding := a.promptBindingLocked(tabID, wire.Approval.ID, nil)
			wire.Approval.ID = string(promptID)
			reason := localStringPointerOrNil(wire.Approval.Reason)
			state.pendingPrompt = &runtimeapi.PendingPrompt{
				Kind: runtimeapi.PromptApproval,
				Approval: &runtimeapi.ApprovalPrompt{
					ID: promptID, Tool: wire.Approval.Tool, Subject: wire.Approval.Subject,
					Reason: reason, Fresh: wire.Approval.Fresh,
					AllowedDecisions: []runtimeapi.PromptDecision{
						runtimeapi.DecisionAllowOnce, runtimeapi.DecisionAllowSession,
						runtimeapi.DecisionAllowPersistent, runtimeapi.DecisionDeny,
					},
				},
			}
			a.prompts[promptID] = binding
		}
	case "ask_request":
		if wire.Ask != nil {
			questions := make(map[runtimeapi.QuestionID]string, len(wire.Ask.Questions))
			promptID, binding := a.promptBindingLocked(tabID, wire.Ask.ID, questions)
			projected := make([]runtimeapi.AskQuestion, 0, len(wire.Ask.Questions))
			for index := range wire.Ask.Questions {
				question := &wire.Ask.Questions[index]
				questionID := a.questionIDLocked(promptID, question.ID, binding.questions)
				question.ID = string(questionID)
				options := make([]runtimeapi.AskOption, 0, len(question.Options))
				for _, option := range question.Options {
					options = append(options, runtimeapi.AskOption{Label: option.Label, Description: localStringPointerOrNil(option.Description)})
				}
				projected = append(projected, runtimeapi.AskQuestion{
					ID: questionID, Header: question.Header, Prompt: localStringPointer(question.Prompt),
					Options: options, Multi: question.Multi,
				})
			}
			wire.Ask.ID = string(promptID)
			state.pendingPrompt = &runtimeapi.PendingPrompt{Kind: runtimeapi.PromptAsk, Ask: &runtimeapi.AskPrompt{ID: promptID, Questions: projected}}
			a.prompts[promptID] = binding
		}
	}

	state.liveEvents = append(state.liveEvents, wire)
	if len(state.liveEvents) > 512 {
		state.liveEvents = append([]eventwire.Event(nil), state.liveEvents[len(state.liveEvents)-512:]...)
	}
	if state.subscribed {
		a.queue = append(a.queue, runtimeapi.Event{Session: ref, TurnID: turnID, OperationID: operationID, Value: wire})
		select {
		case a.wake <- struct{}{}:
		default:
		}
	}
	if wire.Kind == "turn_done" {
		state.currentTurn = ""
		state.cancelRequested = false
		state.pendingPrompt = nil
		state.liveEvents = []eventwire.Event{}
		a.clearPromptsForTabLocked(tabID)
	}
	if wire.Kind == "operation_done" {
		state.currentOperation = nil
		state.cancelRequested = false
		state.liveEvents = []eventwire.Event{}
	}
}

func (a *LocalTargetAdapter) promptBindingLocked(tabID, rawID string, questions map[runtimeapi.QuestionID]string) (runtimeapi.PromptID, localPromptBinding) {
	key := tabID + "\x00" + rawID
	if existing := a.rawPrompts[key]; existing != "" {
		binding := a.prompts[existing]
		if binding.questions == nil && questions != nil {
			binding.questions = questions
		}
		return existing, binding
	}
	id, err := newLocalOpaqueID("local_prompt")
	if err != nil {
		// crypto/rand failure leaves a deterministic, target-private identity;
		// the raw prompt remains hidden and a replay retains the same binding.
		id = localOpaqueID("local_prompt", key)
	}
	promptID := runtimeapi.PromptID(id)
	binding := localPromptBinding{tabID: tabID, rawPromptID: rawID, questions: questions}
	a.rawPrompts[key] = promptID
	a.prompts[promptID] = binding
	return promptID, binding
}

func (a *LocalTargetAdapter) questionIDLocked(promptID runtimeapi.PromptID, rawID string, questions map[runtimeapi.QuestionID]string) runtimeapi.QuestionID {
	for opaque, raw := range questions {
		if raw == rawID {
			return opaque
		}
	}
	id, err := newLocalOpaqueID("local_question")
	if err != nil {
		id = localOpaqueID("local_question", string(promptID)+"\x00"+rawID)
	}
	questionID := runtimeapi.QuestionID(id)
	questions[questionID] = rawID
	return questionID
}

func (a *LocalTargetAdapter) clearPromptsForTabLocked(tabID string) {
	for promptID, binding := range a.prompts {
		if binding.tabID != tabID {
			continue
		}
		delete(a.prompts, promptID)
		delete(a.rawPrompts, tabID+"\x00"+binding.rawPromptID)
	}
}

func localStringPointer(value string) *string {
	copyValue := value
	return &copyValue
}

func localStringPointerOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return localStringPointer(value)
}

func cloneRuntimePendingPrompt(in *runtimeapi.PendingPrompt) *runtimeapi.PendingPrompt {
	if in == nil {
		return nil
	}
	out := *in
	if in.Approval != nil {
		approval := *in.Approval
		if in.Approval.Reason != nil {
			approval.Reason = localStringPointer(*in.Approval.Reason)
		}
		approval.AllowedDecisions = append([]runtimeapi.PromptDecision(nil), in.Approval.AllowedDecisions...)
		out.Approval = &approval
	}
	if in.Ask != nil {
		ask := *in.Ask
		ask.Questions = make([]runtimeapi.AskQuestion, len(in.Ask.Questions))
		for index, question := range in.Ask.Questions {
			copyQuestion := question
			if question.Prompt != nil {
				copyQuestion.Prompt = localStringPointer(*question.Prompt)
			}
			copyQuestion.Options = make([]runtimeapi.AskOption, len(question.Options))
			for optionIndex, option := range question.Options {
				copyOption := option
				if option.Description != nil {
					copyOption.Description = localStringPointer(*option.Description)
				}
				copyQuestion.Options[optionIndex] = copyOption
			}
			ask.Questions[index] = copyQuestion
		}
		out.Ask = &ask
	}
	return &out
}

func (a *LocalTargetAdapter) snapshotLocked(tab *WorkspaceTab, state *localRuntimeSession, historyTurns int) (runtimeapi.SessionSnapshot, error) {
	a.app.mu.RLock()
	if a.app.tabs[tab.ID] != tab || tab.Ctrl == nil || !tab.Ready {
		a.app.mu.RUnlock()
		return runtimeapi.SessionSnapshot{}, a.app.workspaceNotReadyErr(tab)
	}
	ctrl := tab.Ctrl
	title := tab.TopicTitle
	if strings.TrimSpace(title) == "" {
		title = "Session"
	}
	topicID := tab.TopicID
	model := tab.model
	effort := ""
	if tab.effort != nil {
		effort = *tab.effort
	}
	profile := runtimeapi.ResolvedProfile{
		Model: model, Effort: effort, CollaborationMode: currentTabCollaborationMode(tab),
		TokenMode: currentTabTokenMode(tab), ToolApprovalMode: currentTabToolApprovalMode(tab),
	}
	goal := currentTabGoal(tab)
	goalStatus := currentTabGoalStatus(tab)
	a.app.mu.RUnlock()

	status := ctrl.RuntimeStatus()
	if status.Running && state.currentTurn == "" {
		id, err := newLocalOpaqueID("local_turn")
		if err != nil {
			return runtimeapi.SessionSnapshot{}, err
		}
		state.currentTurn = runtimeapi.TurnID(id)
	}
	if !status.Running && state.currentOperation == nil {
		state.currentTurn = ""
		state.cancelRequested = false
	}
	if status.PendingPrompt && state.pendingPrompt == nil {
		return runtimeapi.SessionSnapshot{}, errors.New("Local controller reports a pending prompt but did not replay its prompt payload")
	}

	checkpointValues := a.app.CheckpointsForTab(tab.ID)
	checkpointIDs, err := a.syncLocalCheckpointsLocked(state.ref, checkpointValues)
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	page := a.app.HistoryPageForTab(tab.ID, 0, historyTurns)
	history := projectLocalHistory(page, checkpointIDs)
	if history.HasOlder {
		cursor, cursorErr := newLocalOpaqueID("local_history_cursor")
		if cursorErr != nil {
			return runtimeapi.SessionSnapshot{}, cursorErr
		}
		history.Next = runtimeapi.Cursor(cursor)
		a.v1.cursors[history.Next] = localRuntimeCursor{kind: "session/history", binding: string(state.ref.SessionID), revision: fmt.Sprint(history.TotalTurns), offset: history.StartTurn}
	}
	todos := projectLocalTodos(ctrl)
	usedTokens, windowTokens := ctrl.ContextSnapshot()
	contextView, err := runtimeservice.ProjectContext(runtimeservice.ContextSource{
		UsedTokens: usedTokens, WindowTokens: windowTokens, LastUsage: ctrl.LastUsage(), Telemetry: tab.telemetrySnapshot(),
	})
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	projectedJobs, err := runtimeservice.ProjectJobs(ctrl.Jobs())
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	jobs := make([]runtimeapi.Job, len(projectedJobs))
	for index, job := range projectedJobs {
		raw := string(job.ID)
		job.ID = runtimeapi.JobID(localOpaqueID("local_job", string(state.ref.SessionID)+"\x00"+raw))
		a.v1.jobs[job.ID] = localJobBinding{session: state.ref, rawID: raw}
		jobs[index] = job
	}
	checkpoints := projectLocalCheckpoints(checkpointValues, checkpointIDs)
	runtimeState := runtimeapi.RuntimeState{
		Running: status.Running || state.currentOperation != nil, CancelRequested: status.CancelRequested || state.cancelRequested,
		LiveEvents: append([]eventwire.Event(nil), state.liveEvents...),
	}
	if state.currentTurn != "" {
		runtimeState.CurrentTurn = &runtimeapi.TurnState{ID: state.currentTurn, CancelRequested: runtimeState.CancelRequested}
	}
	if state.currentOperation != nil {
		runtimeState.CurrentOperation = &runtimeapi.OperationState{ID: state.currentOperation.id, Kind: string(state.currentOperation.kind), CancelRequested: state.cancelRequested}
	}
	var goalPointer *string
	if strings.TrimSpace(goal) != "" {
		goalPointer = localStringPointer(goal)
	}
	return runtimeapi.SessionSnapshot{
		Session: state.ref, TopicID: runtimeapi.TopicID(localOpaqueID("local_topic", string(state.ref.WorkspaceID)+"\x00"+topicID)),
		Title: title, Profile: profile, Goal: goalPointer, GoalStatus: runtimeapi.GoalStatus(goalStatus),
		Capabilities: localRuntimeCapabilities(), Runtime: runtimeState, History: history,
		PendingPrompt: cloneRuntimePendingPrompt(state.pendingPrompt), Todos: todos,
		Context: contextView, Jobs: jobs, Checkpoints: checkpoints,
	}, nil
}

func projectLocalHistory(page HistoryPage, checkpointIDs map[int]runtimeapi.CheckpointID) runtimeapi.HistoryPage {
	messages := make([]runtimeapi.HistoryMessage, 0, len(page.Messages))
	for _, message := range page.Messages {
		projected := runtimeapi.HistoryMessage{
			Role: message.Role, Content: localStringPointer(message.Content), Code: message.Code,
			Reasoning: localStringPointerOrNil(message.Reasoning), WorkDurationMillis: message.WorkDurationMs,
			MemoryCitations: eventwire.ToWireMemoryCitations(message.MemoryCitations), Level: message.Level,
			ToolCallID: message.ToolCallID, ToolName: message.ToolName,
			ToolResultArchived: message.ToolResultArchived, ToolResultError: localStringPointerOrNil(message.ToolResultError),
			Pending: message.Pending, Trigger: message.Trigger, Messages: message.Messages,
			Summary: localStringPointerOrNil(message.Summary), Archive: localStringPointerOrNil(message.Archive),
		}
		projected.Detail = localStringPointerOrNil(message.Detail)
		projected.SubmitText = localStringPointerOrNil(message.SubmitText)
		if message.CheckpointTurn != nil {
			projected.CheckpointID = checkpointIDs[*message.CheckpointTurn]
		}
		projected.ToolCalls = make([]runtimeapi.HistoryToolCall, 0, len(message.ToolCalls))
		for _, tool := range message.ToolCalls {
			projected.ToolCalls = append(projected.ToolCalls, runtimeapi.HistoryToolCall{
				ID: tool.ID, Name: tool.Name, Arguments: localStringPointerOrNil(tool.Arguments),
				Subject: tool.Subject, Summary: localStringPointerOrNil(tool.Summary), Diff: localStringPointerOrNil(tool.Diff),
				Added: tool.Added, Removed: tool.Removed, ArgumentsArchived: tool.ArgumentsArchived,
			})
		}
		messages = append(messages, projected)
	}
	result := runtimeapi.HistoryPage{
		Messages: messages, StartTurn: page.StartTurn, EndTurn: page.EndTurn,
		TotalTurns: page.TotalTurns, HasOlder: page.HasOlder,
	}
	return result
}

func projectLocalTodos(ctrl control.SessionAPI) []runtimeapi.TodoItem {
	values := ctrl.Todos()
	out := make([]runtimeapi.TodoItem, 0, len(values))
	for _, value := range values {
		out = append(out, runtimeapi.TodoItem{
			Content: localStringPointerOrNil(value.Content), Status: runtimeapi.TodoStatus(value.Status),
			ActiveForm: value.ActiveForm, Level: value.Level,
		})
	}
	return out
}

func projectLocalCheckpoints(values []CheckpointMeta, known map[int]runtimeapi.CheckpointID) []runtimeapi.Checkpoint {
	out := make([]runtimeapi.Checkpoint, 0, len(values))
	for _, value := range values {
		id := known[value.Turn]
		out = append(out, runtimeapi.Checkpoint{
			ID: id, DisplayTurn: value.Turn, Prompt: localStringPointerOrNil(value.Prompt),
			Files: append([]string(nil), value.Files...), FileCount: value.FileCount,
			FilesTruncated: value.FilesTruncated, CreatedAtMillis: value.Time,
			CanCode: value.CanCode, CanConversation: value.CanConversation,
		})
	}
	return out
}

func localOpaqueID(prefix, seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return prefix + "_" + hex.EncodeToString(sum[:16])
}

func newLocalOpaqueID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func (a *LocalTargetAdapter) refreshSessionsLocked() {
	a.app.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.app.tabs))
	for _, tab := range a.app.tabs {
		if tab != nil {
			tabs = append(tabs, tab)
		}
	}
	a.app.mu.RUnlock()
	seen := make(map[string]bool, len(tabs))
	for _, tab := range tabs {
		ref := runtimeapi.SessionRef{WorkspaceID: localWorkspaceID(tab), SessionID: localSessionID(tab)}
		seen[tab.ID] = true
		if old, ok := a.tabSessions[tab.ID]; ok && old != ref {
			delete(a.sessions, old)
		}
		a.tabSessions[tab.ID] = ref
		state := a.sessions[ref]
		if state == nil {
			state = &localRuntimeSession{tabID: tab.ID, ref: ref, liveEvents: []eventwire.Event{}}
			a.sessions[ref] = state
		} else {
			state.tabID = tab.ID
		}
	}
	for tabID, ref := range a.tabSessions {
		if !seen[tabID] {
			delete(a.tabSessions, tabID)
			delete(a.sessions, ref)
		}
	}
}

func (a *LocalTargetAdapter) SessionRefForTab(tabID string) (runtimeapi.SessionRef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return runtimeapi.SessionRef{}, ErrLocalTargetSuspended
	}
	a.refreshSessionsLocked()
	ref, ok := a.tabSessions[tabID]
	if !ok {
		return runtimeapi.SessionRef{}, ErrLocalSessionUnknown
	}
	return ref, nil
}

func (a *LocalTargetAdapter) sessionLocked(ref runtimeapi.SessionRef) (*localRuntimeSession, *WorkspaceTab, error) {
	if !ref.Valid() {
		return nil, nil, errors.New("workspaceId and sessionId are required")
	}
	a.refreshSessionsLocked()
	state := a.sessions[ref]
	if state == nil {
		return nil, nil, ErrLocalSessionUnknown
	}
	a.app.mu.RLock()
	tab := a.app.tabs[state.tabID]
	a.app.mu.RUnlock()
	if tab == nil {
		return nil, nil, ErrLocalSessionUnknown
	}
	return state, tab, nil
}

func localRuntimeCapabilities() runtimeapi.Capabilities {
	return runtimeapi.Capabilities{
		HostConfig: true, WorkspaceBrowse: true, SessionCreate: true,
		SessionAttach: true, ComposerSubmit: true, TurnSteer: true,
		TurnCancel: true, PromptApprove: true, PromptAnswer: true,
		Features: runtimeapi.Features{
			CoreSession: true, PrimaryFileQueries: true, UserShell: true,
			JobCancel: true, Memory: true, Research: true, LocalPathOperations: true,
		},
		Limits: runtimeapi.Limits{
			HistoryMaxTurns: runtimeapi.HistoryMaxTurns, PageDefaultItems: runtimeapi.PageDefaultItems,
			PageMaxItems: runtimeapi.PageMaxItems, SearchDefaultItems: runtimeapi.SearchDefaultItems,
			SearchMaxItems: runtimeapi.SearchMaxItems, SearchMaxVisitedItems: runtimeapi.SearchMaxVisitedItems,
			PreviewBytes: runtimeapi.PreviewBytes, GitHistoryCommits: runtimeapi.GitHistoryCommits,
			GitPatchBytes: runtimeapi.GitPatchBytes,
		},
	}
}

func (a *LocalTargetAdapter) AttachAndSubscribe(ctx context.Context, input runtimeapi.AttachAndSubscribeInput) (runtimeapi.SessionSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := runtimeapi.ValidateHistoryTurns(input.HistoryTurns); err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	opened, err := a.ensureSessionOpen(ctx, input.Session)
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	input.Session = opened
	// Replay first so Local's existing pending-prompt owner populates the
	// adapter cache. The event barrier below then makes subscription plus the
	// snapshot atomic with respect to every subsequent event.
	a.mu.Lock()
	_, tab, err := a.sessionLocked(input.Session)
	a.mu.Unlock()
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	if err := a.beginAppAdmission(); err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	if ctrl := a.app.controllerForTab(tab); ctrl != nil {
		ctrl.ReplayPendingPrompts()
	}
	a.app.endLocalTargetAdmission()

	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-ctx.Done():
		return runtimeapi.SessionSnapshot{}, ctx.Err()
	default:
	}
	state, tab, err := a.sessionLocked(input.Session)
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	snapshot, err := a.snapshotLocked(tab, state, input.HistoryTurns)
	if err != nil {
		return runtimeapi.SessionSnapshot{}, err
	}
	state.subscribed = true
	return snapshot, nil
}

func (a *LocalTargetAdapter) beginAppAdmission() error {
	if a == nil || a.app == nil {
		return ErrLocalTargetSuspended
	}
	return a.app.beginLocalTargetAdmission()
}

func (a *LocalTargetAdapter) clearTurn(ref runtimeapi.SessionRef, turnID runtimeapi.TurnID) {
	a.mu.Lock()
	if state := a.sessions[ref]; state != nil && state.currentTurn == turnID {
		state.currentTurn = ""
		state.cancelRequested = false
	}
	a.mu.Unlock()
}

func (a *LocalTargetAdapter) SteerTurn(ctx context.Context, input runtimeapi.SteerInput) error {
	if strings.TrimSpace(input.Text) == "" {
		return errors.New("steer text is required")
	}
	a.mu.Lock()
	state, tab, err := a.sessionLocked(input.Session)
	if err == nil && (input.TurnID == "" || state.currentTurn != input.TurnID) {
		err = ErrLocalTurnMismatch
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return a.app.SteerForTab(tab.ID, input.Text)
}

func (a *LocalTargetAdapter) CancelTurn(ctx context.Context, input runtimeapi.CancelTurnInput) error {
	a.mu.Lock()
	state, tab, err := a.sessionLocked(input.Session)
	if err == nil && (input.TurnID == "" || state.currentTurn != input.TurnID) {
		err = ErrLocalTurnMismatch
	}
	if err == nil {
		state.cancelRequested = true
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return a.app.cancelLocalTab(tab.ID)
}

func (a *LocalTargetAdapter) ApprovePrompt(ctx context.Context, input runtimeapi.ApproveInput) error {
	a.mu.Lock()
	_, tab, err := a.sessionLocked(input.Session)
	binding, ok := a.prompts[input.PromptID]
	if err == nil && (!ok || binding.tabID != tab.ID) {
		err = errors.New("Local approval prompt ID is not pending for this session")
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	allow, session, persist := false, false, false
	switch input.Decision {
	case runtimeapi.DecisionAllowOnce:
		allow = true
	case runtimeapi.DecisionAllowSession:
		allow, session = true, true
	case runtimeapi.DecisionAllowPersistent:
		allow, session, persist = true, true, true
	case runtimeapi.DecisionDeny:
	default:
		return errors.New("unsupported prompt decision")
	}
	if err := a.app.approveLocalPrompt(tab.ID, binding.rawPromptID, allow, session, persist); err != nil {
		return err
	}
	a.consumePrompt(input.Session, input.PromptID)
	return nil
}

func (a *LocalTargetAdapter) AnswerPrompt(ctx context.Context, input runtimeapi.AnswerInput) error {
	a.mu.Lock()
	_, tab, err := a.sessionLocked(input.Session)
	binding, ok := a.prompts[input.PromptID]
	if err == nil && (!ok || binding.tabID != tab.ID || binding.questions == nil) {
		err = errors.New("Local ask prompt ID is not pending for this session")
	}
	answers := make([]QuestionAnswer, 0, len(input.Answers))
	if err == nil {
		seen := make(map[runtimeapi.QuestionID]bool, len(input.Answers))
		for _, answer := range input.Answers {
			rawID, exists := binding.questions[answer.QuestionID]
			if !exists || seen[answer.QuestionID] {
				err = errors.New("ask answer contains an unknown or duplicate question ID")
				break
			}
			seen[answer.QuestionID] = true
			answers = append(answers, QuestionAnswer{QuestionID: rawID, Selected: append([]string(nil), answer.Selected...)})
		}
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if err := a.app.answerLocalPrompt(tab.ID, binding.rawPromptID, answers); err != nil {
		return err
	}
	a.consumePrompt(input.Session, input.PromptID)
	return nil
}

func (a *LocalTargetAdapter) consumePrompt(ref runtimeapi.SessionRef, promptID runtimeapi.PromptID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	binding, ok := a.prompts[promptID]
	if !ok {
		return
	}
	delete(a.prompts, promptID)
	delete(a.rawPrompts, binding.tabID+"\x00"+binding.rawPromptID)
	state := a.sessions[ref]
	if state == nil || state.pendingPrompt == nil {
		return
	}
	currentID := runtimeapi.PromptID("")
	if state.pendingPrompt.Approval != nil {
		currentID = state.pendingPrompt.Approval.ID
	}
	if state.pendingPrompt.Ask != nil {
		currentID = state.pendingPrompt.Ask.ID
	}
	if currentID == promptID {
		state.pendingPrompt = nil
	}
}
