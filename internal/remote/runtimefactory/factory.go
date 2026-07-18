// Package runtimefactory assembles daemon-owned Controllers from opaque Remote
// catalog targets. It is the production bridge from transport-neutral Host
// runtime ownership to the same boot.Build composition root used by Local
// frontends.
package runtimefactory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/sessiondisplay"
)

type Builder func(context.Context, boot.Options) (control.SessionAPI, error)

type Options struct {
	Resolver             catalog.RuntimeTargetResolver
	Builder              Builder
	Stderr               io.Writer
	OnDisplayRecordError func(error)
}

// Factory holds process-local Session file leases by stable Remote target.
// Runtime replacement builds the new Controller before stopping the old one;
// reference-counting the target lease permits that overlap without allowing a
// second process to write the same transcript.
type Factory struct {
	resolver                 catalog.RuntimeTargetResolver
	builder                  Builder
	stderr                   io.Writer
	reportDisplayRecordError func(error)
	reportMu                 sync.Mutex

	leaseMu sync.Mutex
	leases  map[protocol.RuntimeTarget]*targetLease
}

type targetLease struct {
	path   string
	keeper *control.SessionLeaseKeeper
	refs   int
}

func New(options Options) (*Factory, error) {
	if nilutil.IsNil(options.Resolver) {
		return nil, errors.New("Remote runtime target resolver is required")
	}
	builder := options.Builder
	if builder == nil {
		builder = func(ctx context.Context, options boot.Options) (control.SessionAPI, error) {
			return boot.Build(ctx, options)
		}
	}
	reportDisplayRecordError := options.OnDisplayRecordError
	if reportDisplayRecordError == nil && options.Stderr != nil {
		reportDisplayRecordError = func(err error) {
			_, _ = fmt.Fprintf(options.Stderr, "reasonix Remote display sidecar: %v\n", err)
		}
	}
	return &Factory{
		resolver: options.Resolver, builder: builder, stderr: options.Stderr,
		reportDisplayRecordError: reportDisplayRecordError,
		leases:                   make(map[protocol.RuntimeTarget]*targetLease),
	}, nil
}

func (f *Factory) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	controller, _, err := f.CreateControllerWithRecovery(ctx, target, sink)
	return controller, err
}

// CreateControllerWithRecovery is the production recovery-aware construction
// path consumed by RuntimeManager. The recovery fact is captured from the
// Controller's Resume operation before the durable in-flight marker is cleared;
// a later catalog metadata read cannot reconstruct that exact boundary.
func (f *Factory) CreateControllerWithRecovery(
	ctx context.Context,
	target protocol.RuntimeTarget,
	sink event.Sink,
) (control.SessionAPI, control.SessionResumeState, error) {
	if f == nil || nilutil.IsNil(f.resolver) || f.builder == nil {
		return nil, control.SessionResumeState{}, errors.New("Remote runtime factory is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, control.SessionResumeState{}, err
	}
	if err := target.Validate(); err != nil {
		return nil, control.SessionResumeState{}, err
	}
	resolved, err := f.resolver.ResolveRuntimeTarget(ctx, target)
	if err != nil {
		return nil, control.SessionResumeState{}, err
	}
	if err := validateResolvedTarget(target, resolved); err != nil {
		return nil, control.SessionResumeState{}, err
	}
	releaseLease, err := f.retainLease(target, resolved.SessionPath)
	if err != nil {
		return nil, control.SessionResumeState{}, err
	}
	releaseOnFailure := true
	defer func() {
		if releaseOnFailure {
			releaseLease()
		}
	}()

	session, err := resolved.LoadSession()
	if err != nil {
		return nil, control.SessionResumeState{}, fmt.Errorf("load Remote Session %s: %w", target.SessionID, err)
	}
	effort := resolved.ResolvedProfile.Effort
	controller, err := f.builder(ctx, boot.Options{
		Model:                resolved.ResolvedProfile.Model,
		RequireKey:           false,
		Sink:                 sink,
		EffortOverride:       &effort,
		AdditionalDirs:       append([]string(nil), resolved.AdditionalDirs...),
		Stderr:               f.stderr,
		WorkspaceRoot:        resolved.WorkspaceRoot,
		TokenMode:            string(resolved.ResolvedProfile.TokenMode),
		SessionDir:           resolved.SessionDir,
		HeadlessApprovalMode: string(resolved.ResolvedProfile.ToolApprovalMode),
	})
	if err != nil {
		return nil, control.SessionResumeState{}, fmt.Errorf("build Remote Session %s Controller: %w", target.SessionID, err)
	}
	if nilutil.IsNil(controller) {
		return nil, control.SessionResumeState{}, errors.New("Remote boot builder returned a nil Controller")
	}
	// Display metadata is auxiliary: an accepted turn and its canonical transcript
	// must not fail because its shorter UI text could not be persisted. The stable
	// resolved paths belong to this runtime target and cannot drift with a later
	// Controller path mutation. Failures are reported through the injected hook (or
	// Stderr when configured) and otherwise remain explicitly best-effort.
	displayDir, displayPath := resolved.SessionDir, resolved.SessionPath
	controller.SetDisplayRecorder(func(content, display string) {
		if err := sessiondisplay.Record(displayDir, displayPath, content, display); err != nil {
			f.reportDisplayError(err)
		}
	})
	controller.EnableInteractiveApproval()
	controller.SetPlanMode(resolved.ResolvedProfile.CollaborationMode == protocol.CollaborationPlan)
	controller.SetToolApprovalMode(string(resolved.ResolvedProfile.ToolApprovalMode))
	// The append-only event log can be ahead of the zero-byte transcript anchor
	// when the process dies during a Session's first accepted Turn. That log
	// contains the appended user/assistant tail but not the bootstrap system
	// message which already existed in memory. Refresh or prepend boot.Build's
	// current leading system message while preserving the loaded Session's CAS
	// baseline. Refresh is required when a Remote profile/token-mode rebuild
	// changes the current contract; prepend repairs the zero-byte crash anchor.
	// Without the anchor, checkpoint message indexes shift by one and the next
	// Remote snapshot cannot be projected after a genuine crash.
	session, err = withRemoteSystemPromptAnchor(session, controller.History())
	if err != nil {
		controller.Close()
		return nil, control.SessionResumeState{}, fmt.Errorf("bind Remote Session %s system prompt: %w", target.SessionID, err)
	}
	// Validate and enable the crash-safe admission contract before Resume can
	// consume an in-flight marker. A custom builder that cannot satisfy the
	// Remote contract must fail without destroying the durable recovery fact.
	durable, ok := controller.(control.DurableTurnAdmission)
	if !ok {
		closeControllerAfterBuildFailure(controller)
		return nil, control.SessionResumeState{}, errors.New("Remote Controller does not support durable Turn admission")
	}
	recovery, ok := controller.(control.RecoveryLifecycle)
	if !ok {
		closeControllerAfterBuildFailure(controller)
		return nil, control.SessionResumeState{}, errors.New("Remote Controller does not support crash recovery lifecycle")
	}
	if err := durable.EnableDurableTurnAdmission(); err != nil {
		closeControllerAfterBuildFailure(controller)
		return nil, control.SessionResumeState{}, fmt.Errorf("enable Remote durable Turn admission: %w", err)
	}
	resumeState, err := recovery.ResumeWithRecovery(session, resolved.SessionPath)
	if err != nil {
		// Recovery is part of runtime construction, not best-effort startup
		// maintenance. Publishing a Controller whose transcript repair or marker
		// clear did not commit would let a new Turn overwrite the only durable
		// proof of the interrupted predecessor.
		closeControllerAfterBuildFailure(controller)
		return nil, control.SessionResumeState{}, fmt.Errorf("recover Remote Session %s: %w", target.SessionID, err)
	}

	releaseOnFailure = false
	return &leasedController{SessionAPI: controller, release: releaseLease}, resumeState, nil
}

func closeControllerAfterBuildFailure(controller control.SessionAPI) {
	if nilutil.IsNil(controller) {
		return
	}
	// Cleanup must not replace the precise construction error with a panic from
	// an injected/partially-built Controller. The target lease is released by the
	// caller's deferred failure path either way.
	defer func() { _ = recover() }()
	controller.Close()
}

func withRemoteSystemPromptAnchor(session *agent.Session, builtHistory []provider.Message) (*agent.Session, error) {
	if session == nil || len(builtHistory) == 0 || builtHistory[0].Role != provider.RoleSystem || strings.TrimSpace(builtHistory[0].Content) == "" {
		return session, nil
	}
	fresh := builtHistory[0]
	messages := session.Snapshot()
	if len(messages) > 0 && messages[0].Role == provider.RoleSystem {
		messages[0] = fresh
	} else {
		messages = append([]provider.Message{fresh}, messages...)
	}
	refreshed, ok := session.CloneWithMessagesIfCompatible(messages)
	if !ok || refreshed == nil {
		return nil, errors.New("loaded history is incompatible with a leading system prompt refresh")
	}
	return refreshed, nil
}

func (f *Factory) reportDisplayError(err error) {
	if f == nil || err == nil || f.reportDisplayRecordError == nil {
		return
	}
	// Controllers can finish turns concurrently. Serialize even injected
	// reporters so a plain bytes.Buffer/io.Writer remains race-safe in tests and
	// small daemon embeddings.
	f.reportMu.Lock()
	defer f.reportMu.Unlock()
	f.reportDisplayRecordError(err)
}

func validateResolvedTarget(target protocol.RuntimeTarget, resolved catalog.ResolvedSession) error {
	if resolved.Target != target {
		return errors.New("Remote catalog resolved a different target")
	}
	for label, path := range map[string]string{
		"workspace root": resolved.WorkspaceRoot,
		"Session dir":    resolved.SessionDir,
		"Session path":   resolved.SessionPath,
	} {
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("Remote %s must be a canonical absolute path", label)
		}
	}
	relative, err := filepath.Rel(resolved.SessionDir, resolved.SessionPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("Remote Session path is outside its Session directory")
	}
	seen := make(map[string]struct{}, len(resolved.AdditionalDirs)+1)
	seen[resolved.WorkspaceRoot] = struct{}{}
	for _, path := range resolved.AdditionalDirs {
		if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("Remote additional directory must be a canonical absolute path")
		}
		if _, exists := seen[path]; exists {
			return errors.New("Remote additional directories must be unique and exclude the primary workspace")
		}
		seen[path] = struct{}{}
	}
	profile := resolved.ResolvedProfile
	if strings.TrimSpace(profile.Model) == "" || strings.TrimSpace(profile.Effort) == "" {
		return errors.New("Remote resolved profile requires model and effort")
	}
	switch profile.CollaborationMode {
	case protocol.CollaborationNormal, protocol.CollaborationPlan, protocol.CollaborationGoal:
	default:
		return errors.New("Remote resolved profile has an invalid collaboration mode")
	}
	switch profile.TokenMode {
	case protocol.TokenFull, protocol.TokenEconomy, protocol.TokenDelivery:
	default:
		return errors.New("Remote resolved profile has an invalid token mode")
	}
	switch profile.ToolApprovalMode {
	case protocol.ToolApprovalAsk, protocol.ToolApprovalAuto, protocol.ToolApprovalYOLO:
	default:
		return errors.New("Remote resolved profile has an invalid tool approval mode")
	}
	return nil
}

func (f *Factory) retainLease(target protocol.RuntimeTarget, path string) (func(), error) {
	f.leaseMu.Lock()
	defer f.leaseMu.Unlock()
	if current := f.leases[target]; current != nil {
		if current.path != path {
			return nil, errors.New("Remote target changed Session path during runtime replacement")
		}
		current.refs++
		return f.releaseFunc(target, current), nil
	}
	keeper := control.NewSessionLeaseKeeper()
	if err := keeper.Rebind(path); err != nil {
		return nil, fmt.Errorf("acquire Remote Session writer lease: %w", err)
	}
	current := &targetLease{path: path, keeper: keeper, refs: 1}
	f.leases[target] = current
	return f.releaseFunc(target, current), nil
}

func (f *Factory) releaseFunc(target protocol.RuntimeTarget, retained *targetLease) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			f.leaseMu.Lock()
			current := f.leases[target]
			if current == retained {
				current.refs--
				if current.refs == 0 {
					delete(f.leases, target)
					current.keeper.Release()
				}
			}
			f.leaseMu.Unlock()
		})
	}
}

type leasedController struct {
	control.SessionAPI
	release func()
	once    sync.Once
}

func (c *leasedController) finish(close func()) {
	c.once.Do(func() {
		defer c.release()
		close()
	})
}

func (c *leasedController) Close() {
	c.finish(c.SessionAPI.Close)
}

func (c *leasedController) CloseAfterDestroy() {
	c.finish(c.SessionAPI.CloseAfterDestroy)
}

func (c *leasedController) ReleaseResources() {
	c.finish(c.SessionAPI.ReleaseResources)
}

func (c *leasedController) ResumeWithRecovery(session *agent.Session, path string) (control.SessionResumeState, error) {
	if recovery, ok := c.SessionAPI.(control.RecoveryLifecycle); ok {
		return recovery.ResumeWithRecovery(session, path)
	}
	c.SessionAPI.Resume(session, path)
	return control.SessionResumeState{}, nil
}

func (c *leasedController) PrepareRuntimeShutdown() {
	if recovery, ok := c.SessionAPI.(control.RecoveryLifecycle); ok {
		recovery.PrepareRuntimeShutdown()
	}
}

func (c *leasedController) EnableDurableTurnAdmission() error {
	if durable, ok := c.SessionAPI.(control.DurableTurnAdmission); ok {
		return durable.EnableDurableTurnAdmission()
	}
	return errors.New("Remote Controller does not support durable Turn admission")
}

func (c *leasedController) PrepareDurableTurn(input control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	if durable, ok := c.SessionAPI.(control.DurableTurnAdmission); ok {
		return durable.PrepareDurableTurn(input)
	}
	return nil, errors.New("Remote Controller does not support durable Turn admission")
}

// ApplyToolApprovalMode preserves Controller's typed prompt-resolution result
// through the lease wrapper. SessionAPI intentionally keeps the legacy void
// setter, while Remote profile mutation needs the exact Controller prompt IDs
// so its actor can translate only those IDs to opaque PromptIDs.
func (c *leasedController) ApplyToolApprovalMode(mode string) []string {
	if controller, ok := c.SessionAPI.(interface{ ApplyToolApprovalMode(string) []string }); ok {
		return controller.ApplyToolApprovalMode(mode)
	}
	c.SessionAPI.SetToolApprovalMode(mode)
	return nil
}

var _ control.SessionAPI = (*leasedController)(nil)
var _ control.RecoveryLifecycle = (*leasedController)(nil)
var _ control.DurableTurnAdmission = (*leasedController)(nil)
