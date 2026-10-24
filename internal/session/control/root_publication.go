package control

import "context"

func (c *Controller) runWithRootPublication(ctx context.Context, body func(context.Context) error) error {
	path := c.SessionPath()
	if err := body(ctx); err != nil {
		return err
	}
	policy := c.rootEnvironmentPolicy
	if policy == nil || policy.Publish == nil || path == "" || path != c.SessionPath() {
		return nil
	}
	if policy.Generation != c.RuntimeGeneration() {
		return ErrRuntimeDraining
	}
	if err := c.CheckMutationExecution(ctx); err != nil {
		return err
	}
	return policy.Publish(ctx, c)
}
