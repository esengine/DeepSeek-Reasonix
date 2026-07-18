package host

import (
	"context"
	"errors"
	"sort"
	"strings"

	"reasonix/internal/billing"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/jobs"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/provider"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
	"reasonix/internal/sessiontelemetry"
)

var ErrJobCancellationUnavailable = errors.New("remote controller does not implement job cancellation")

// SessionCatalogQuery freezes every Controller-owned catalog getter at one
// actor boundary. The shared RuntimeService then drops command/skill/plugin
// implementation details and derives deterministic safe IDs and revision.
func (r *SessionRuntime) SessionCatalogQuery(
	ctx context.Context,
	query protocol.RuntimeQuery,
	beforeRead func() error,
) (runtimeapi.SessionCatalog, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(query, beforeRead); err != nil {
			return nil, err
		}
		source, err := r.sessionCatalogSource()
		if err != nil {
			return nil, err
		}
		return runtimeservice.ProjectSessionCatalog(source)
	})
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	return value.(runtimeapi.SessionCatalog), nil
}

func (r *SessionRuntime) sessionCatalogSource() (source runtimeservice.SessionCatalogSource, err error) {
	err = safeControllerCall(func() {
		source.CustomCommands = append(source.CustomCommands, r.controller.Commands()...)
		source.Skills = append(source.Skills, r.controller.SlashSkills()...)
		configured := append([]string(nil), r.controller.ConfiguredMCPNames()...)
		disconnected := append([]string(nil), r.controller.DisconnectedMCPNames()...)
		byName := make(map[string]runtimeservice.CatalogMCPSource, len(configured)+len(disconnected))
		for _, name := range append(configured, disconnected...) {
			name = strings.TrimSpace(name)
			if name != "" {
				byName[name] = runtimeservice.CatalogMCPSource{Name: name}
			}
		}
		if host := r.controller.Host(); host != nil {
			for _, server := range host.Servers() {
				byName[server.Name] = runtimeservice.CatalogMCPSource{
					Name: server.Name, Available: true, ToolCount: server.Tools,
				}
			}
			for _, failure := range host.Failures() {
				if _, exists := byName[failure.Name]; !exists {
					byName[failure.Name] = runtimeservice.CatalogMCPSource{Name: failure.Name}
				}
			}
			for _, name := range host.ConnectingServers() {
				if _, exists := byName[name]; !exists {
					byName[name] = runtimeservice.CatalogMCPSource{Name: name}
				}
			}
			for _, prompt := range host.Prompts() {
				source.AdditionalCommands = append(source.AdditionalCommands, runtimeservice.CatalogCommandSource{
					Name: prompt.Name, Description: prompt.Description,
				})
			}
		}
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			source.MCPServers = append(source.MCPServers, byName[name])
		}
	})
	if err != nil {
		return runtimeservice.SessionCatalogSource{}, err
	}
	state, loadErr := pluginpkg.LoadState(config.ReasonixHomeDir())
	if loadErr != nil {
		return runtimeservice.SessionCatalogSource{}, runtimeservice.ErrQueryFailed
	}
	source.Plugins = append(source.Plugins, state.Plugins...)
	return source, nil
}

func (r *SessionRuntime) SessionContextQuery(
	ctx context.Context,
	query protocol.RuntimeQuery,
	beforeRead func() error,
) (runtimeapi.ContextView, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(query, beforeRead); err != nil {
			return nil, err
		}
		var usedTokens, windowTokens int
		var lastUsage *provider.Usage
		if err := safeControllerCall(func() {
			usedTokens, windowTokens = r.controller.ContextSnapshot()
			lastUsage = r.controller.LastUsage()
		}); err != nil {
			return nil, err
		}
		return runtimeservice.ProjectContext(runtimeservice.ContextSource{
			UsedTokens: usedTokens, WindowTokens: windowTokens, LastUsage: lastUsage,
			Telemetry: sessiontelemetry.View(state.readFiles, state.usage, r.telemetryNowMillis()),
		})
	})
	if err != nil {
		return runtimeapi.ContextView{}, err
	}
	return value.(runtimeapi.ContextView), nil
}

func (r *SessionRuntime) ListJobsQuery(
	ctx context.Context,
	params protocol.JobListParams,
	beforeRead func() error,
) (runtimeapi.JobPage, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(params.RuntimeQuery, beforeRead); err != nil {
			return nil, err
		}
		var items []jobs.View
		if err := safeControllerCall(func() { items = append([]jobs.View(nil), r.controller.Jobs()...) }); err != nil {
			return nil, err
		}
		if !state.activeJobsManaged {
			state.activeJobs = len(items)
		}
		limit := 0
		if params.Limit != nil {
			limit = *params.Limit
		}
		return runtimeservice.PageJobs(runtimeservice.RuntimeBinding{
			Session: runtimeapi.SessionRef{
				WorkspaceID: runtimeapi.WorkspaceID(r.target.WorkspaceID),
				SessionID:   runtimeapi.SessionID(r.target.SessionID),
			},
			Incarnation: string(r.epoch),
		}, items, runtimeapi.Cursor(params.Cursor), limit)
	})
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	return value.(runtimeapi.JobPage), nil
}

func (r *SessionRuntime) ComposerSlashArgsQuery(
	ctx context.Context,
	params protocol.ComposerSlashArgsParams,
	beforeRead func() error,
) (runtimeapi.SlashArgsResult, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(params.RuntimeQuery, beforeRead); err != nil {
			return nil, err
		}
		data, err := r.slashArgData()
		if err != nil {
			return nil, err
		}
		return runtimeservice.ProjectSlashArgs(params.Input, data)
	})
	if err != nil {
		return runtimeapi.SlashArgsResult{}, err
	}
	return value.(runtimeapi.SlashArgsResult), nil
}

func (r *SessionRuntime) slashArgData() (data control.ArgData, err error) {
	err = safeControllerCall(func() {
		data.Skills = append(data.Skills, r.controller.Skills()...)
		data.DisabledSkills = append(data.DisabledSkills, r.controller.DisabledSkills()...)
		data.ConfiguredMCP = append(data.ConfiguredMCP, r.controller.ConfiguredMCPNames()...)
		data.DisconnectedMCP = append(data.DisconnectedMCP, r.controller.DisconnectedMCPNames()...)
		data.CurrentModel = strings.TrimSpace(r.controller.ModelRef())
		if host := r.controller.Host(); host != nil {
			data.ServerNames = append(data.ServerNames, host.ServerNames()...)
		}
	})
	if err != nil {
		return control.ArgData{}, err
	}
	plugins, loadErr := pluginpkg.InstalledNames(config.ReasonixHomeDir())
	if loadErr != nil {
		return control.ArgData{}, runtimeservice.ErrQueryFailed
	}
	data.PluginNames = plugins
	cfg, loadErr := config.LoadForRoot(r.workspaceRoot)
	if loadErr != nil {
		return control.ArgData{}, runtimeservice.ErrQueryFailed
	}
	if current, ok := cfg.ResolveModel(data.CurrentModel); ok {
		data.CurrentModel = current.Name + "/" + current.Model
		data.CurrentProvider = current.Name
	}
	seenProviders := make(map[string]struct{}, len(cfg.Providers))
	for index := range cfg.Providers {
		provider := &cfg.Providers[index]
		if !provider.Configured() {
			continue
		}
		if _, exists := seenProviders[provider.Name]; !exists {
			seenProviders[provider.Name] = struct{}{}
			data.ProviderNames = append(data.ProviderNames, provider.Name)
		}
		for _, model := range provider.ChatModelList() {
			data.ModelRefs = append(data.ModelRefs, provider.Name+"/"+model)
		}
	}
	return data, nil
}

// SessionBalanceQuery snapshots admission before and after the potentially
// slow provider request. No actor is blocked on network I/O, and a replacement
// or lease migration during the request invalidates the result.
func (r *SessionRuntime) SessionBalanceQuery(
	ctx context.Context,
	query protocol.RuntimeQuery,
	beforeRead func() error,
) (runtimeapi.BalanceView, error) {
	if _, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(query, beforeRead); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	}); err != nil {
		return runtimeapi.BalanceView{}, err
	}
	var balanceResult *billing.Balance
	var queryErr error
	if err := safeControllerCall(func() { balanceResult, queryErr = r.controller.Balance(ctx) }); err != nil {
		queryErr = err
	}
	if _, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if err := r.preadmitRuntimeQuery(query, beforeRead); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	}); err != nil {
		return runtimeapi.BalanceView{}, err
	}
	return runtimeservice.ProjectBalance(balanceResult, queryErr), nil
}

func (r *SessionRuntime) CancelJobMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.JobCancelParams,
	beforeBegin func() error,
) (protocol.JobCancelResult, error) {
	if registry == nil {
		return protocol.JobCancelResult{}, errors.New("remote idempotency registry is required")
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodJobCancel),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				return nil, err
			}
		}
		if err := r.preadmitSessionMutation(params.SessionMutation); err != nil {
			return nil, err
		}
		attempt, err := registry.Begin(request)
		if err != nil {
			return nil, err
		}
		claim, owns := attempt.Claim()
		if !owns {
			return mutationReplay{attempt: attempt}, nil
		}
		canceller, ok := r.controller.(control.JobCancellation)
		if !ok {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "jobCancel"))
		}
		result := protocol.JobCancelResult{Disposition: protocol.JobNotRunning}
		if canceller.CancelBackgroundJob(string(params.JobID)) {
			result.Disposition = protocol.JobCancelled
		}
		var jobsNow []jobs.View
		if err := safeControllerCall(func() { jobsNow = r.controller.Jobs() }); err == nil && !state.activeJobsManaged {
			state.activeJobs = len(jobsNow)
		}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.JobCancelResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		outcome, err := replay.attempt.Wait(ctx)
		if err != nil {
			return protocol.JobCancelResult{}, err
		}
		var result protocol.JobCancelResult
		if err := outcome.Decode(&result); err != nil {
			return protocol.JobCancelResult{}, err
		}
		return result, nil
	}
	result, ok := value.(protocol.JobCancelResult)
	if !ok {
		return protocol.JobCancelResult{}, errors.New("remote job cancel actor returned an invalid result")
	}
	return result, nil
}

func (r *SessionRuntime) preadmitRuntimeQuery(query protocol.RuntimeQuery, beforeRead func() error) error {
	if beforeRead != nil {
		if err := beforeRead(); err != nil {
			return err
		}
	}
	if query.Target != r.target {
		return ErrInvalidRuntimeTarget
	}
	if query.ExpectedHostEpoch != r.hostEpoch {
		return protocol.MustRemoteError(protocol.ErrStaleHostEpoch, protocol.ErrorOptions{
			Expected: string(r.hostEpoch), Actual: string(query.ExpectedHostEpoch),
		})
	}
	if !r.current.Load() || query.ExpectedRuntimeEpoch != r.epoch {
		target := r.target
		return protocol.MustRemoteError(protocol.ErrStaleRuntimeEpoch, protocol.ErrorOptions{
			Target: &target, Expected: string(r.epoch), Actual: string(query.ExpectedRuntimeEpoch),
		})
	}
	return nil
}

// syncActiveJobsForState keeps close/release decisions tied to the exact
// session-scoped Manager rather than an event-text heuristic.
func (r *SessionRuntime) syncActiveJobsForState(state *runtimeActorState) {
	if state == nil || r.controller == nil || state.activeJobsManaged {
		return
	}
	var values []jobs.View
	if err := safeControllerCall(func() { values = r.controller.Jobs() }); err == nil {
		state.activeJobs = len(values)
	}
}
