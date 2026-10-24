package boot

import (
	"context"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
)

func (b *builder) isolationSourceAuthorizer(ctx context.Context, posture *isolationPosture, grant writeclaim.WritePathSet) isolation.SourceAuthorizer {
	validate, hasExecution := delegation.MutationExecutionValidator(ctx)
	if parent := b.opts.isolatedParent; parent != nil {
		if !hasExecution {
			validate = nil
		}
		controller := posture.ctrl.Load()
		current := integrationAuthorizerWithExecution(b.root, posture, b.tools.gate, b.tools.env.targetAccessCheck(), grant, controller.CheckMutationExecution)
		return nestedSourceAuthorizer(b.root, parent.source, current, parent.authorize, validate)
	}
	authorize := integrationAuthorizer(b.root, agent.ParentSession(ctx), posture, b.tools.gate, b.tools.env.targetAccessCheck(), grant)
	if !hasExecution {
		return authorize
	}
	return func(ctx context.Context, paths []string) error {
		if err := validate(ctx); err != nil {
			return err
		}
		return authorize(ctx, paths)
	}
}

func nestedSourceAuthorizer(candidate, source string, current, ancestor isolation.SourceAuthorizer, validate func(context.Context) error) isolation.SourceAuthorizer {
	return func(ctx context.Context, paths []string) error {
		if candidate == "" || source == "" || current == nil || ancestor == nil || validate == nil {
			return tool.Refusal{Code: isolation.CodeStaleExecution, Message: "nested source execution or ancestor authority is unavailable"}
		}
		if err := validate(ctx); err != nil {
			return err
		}
		mapped, err := mapIntegrationSourcePaths(candidate, source, paths)
		if err != nil {
			return err
		}
		if err := current(ctx, paths); err != nil {
			return err
		}
		return ancestor(ctx, mapped)
	}
}

func mapIntegrationSourcePaths(candidate, source string, paths []string) ([]string, error) {
	return isolation.MapSourcePaths(candidate, source, paths)
}
