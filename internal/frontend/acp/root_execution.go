package acp

import (
	"context"
	"errors"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/session/control"
)

// RootExecutionPreparer preserves the composition root's exact assembly inputs.
type RootExecutionPreparer interface {
	PrepareRootExecution(context.Context, SessionParams, *control.Controller) (*boot.PreparedRootExecution, error)
}

func withSessionEnvironment(params SessionParams, current acpController, path string) SessionParams {
	params.SessionEnvironmentPath = path
	if old, ok := current.(*control.Controller); ok {
		params.RootEnvironment = old.RootEnvironment()
	}
	return params
}

// prepareRootPrompt runs after ACP reserves prompt ownership, before resolving
// slash commands or capturing the driving controller. Config changes then queue
// behind this prompt and cannot replace its runtime during preparation.
func (s *service) prepareRootPrompt(ctx context.Context, sess *acpSession) (cleanup func(), resultErr error) {
	old, ok := sess.currentCtrl().(*control.Controller)
	if !ok || old.RootEnvironment() != nil {
		return func() {}, nil
	}
	mode := old.RootEnvironmentStrategy()
	if mode != "auto" && mode != "isolated" {
		return func() {}, nil
	}
	if mode == "auto" && !old.RootEnvironmentSelectionPending() {
		return func() {}, nil
	}
	factory, ok := s.factory.(RootExecutionPreparer)
	if !ok {
		return nil, errors.New("this ACP host cannot prepare root execution environments")
	}
	release, err := old.BeginRuntimeReplacement()
	if err != nil {
		return nil, err
	}
	defer release()
	sess.mu.Lock()
	params := SessionParams{Cwd: sess.cwd, MCPServers: clonePluginSpecs(sess.mcpServers), AdditionalDirs: append([]string(nil), sess.additionalDirs...), Sink: sess.sink,
		Model: sess.model, EffortOverride: cloneStringPtr(sess.effortOverride), RuntimeProfile: sess.runtimeProfile, Composition: old.CompositionBinding(),
		OnSessionRecovered: s.sessionRecoveredHandler(sess.id), SessionEnvironmentPath: old.SessionPath()}
	lease := sess.lease
	sess.mu.Unlock()
	s.bindClientIO(&params, sess.id)
	prepared, err := factory.PrepareRootExecution(ctx, params, old)
	if err != nil {
		return nil, err
	}
	ok = false
	defer func() {
		if !ok {
			prepared.Close()
		}
	}()
	if prepared.Result != nil {
		if err := prepared.Adopt(ctx); err != nil {
			return nil, err
		}
		next := prepared.Controller
		next.EnableInteractiveApproval()
		next.SetOnSessionRecovered(s.sessionRecoveredHandlerFor(sess.id, next))
		if err := bindACPWriteAuthority(next, lease); err != nil {
			return nil, err
		}
		defer func() {
			if !ok {
				resultErr = errors.Join(resultErr, bindACPWriteAuthority(old, lease))
			}
		}()
		if err := prepared.Publish(func(*boot.BuildResult) error {
			sess.mu.Lock()
			defer sess.mu.Unlock()
			if sess.deleted || sess.ctrl != old || ctx.Err() != nil {
				return errors.New("session changed before root environment publication")
			}
			sess.ctrl = next
			return nil
		}); err != nil {
			return nil, err
		}
		sess.sink.bindCwd(next.WorkspaceRoot())
		s.bindCompositionIO(sess, next)
		old.ReleaseResources()
	}
	ok = true
	return prepared.Close, nil
}
