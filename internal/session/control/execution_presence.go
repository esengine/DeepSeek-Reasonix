package control

import "context"

// ExecutionPresence registers active work independently of filesystem leases.
// The returned release runs after the foreground turn and its background jobs.
type ExecutionPresence func(context.Context, string, bool) (func(), error)

func (c *Controller) persistTurnAdmission(ctx context.Context, use *workspaceActivityUse) error {
	if err := c.persistCompositionAdmission(); err != nil {
		return err
	}
	var releases []func()
	if c.rootEnvironment != nil {
		retained, err := c.rootEnvironment.Retain()
		if err != nil {
			return err
		}
		releases = append(releases, func() { _ = retained.Close() })
	}
	ok := false
	defer func() {
		if !ok {
			for _, release := range releases {
				release()
			}
		}
	}()
	if c.executionPresence != nil {
		release, err := c.executionPresence(ctx, c.SessionPath(), c.observe == nil)
		if err != nil {
			return err
		}
		if release != nil {
			releases = append(releases, release)
		}
	}
	if len(releases) > 0 {
		previous := use.releaseReference
		use.releaseReference = func() {
			defer previous()
			for _, release := range releases {
				release()
			}
		}
	}
	ok = true
	return nil
}
