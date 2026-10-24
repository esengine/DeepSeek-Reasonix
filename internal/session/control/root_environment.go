package control

import (
	"context"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/state/sessionstore"
)

// RootEnvironment is borrowed until the caller retains its own runtime reference.
func (c *Controller) RootEnvironment() *rootenv.Environment { return c.rootEnvironment }

func (c *Controller) CheckSessionEnvironment(path string) error {
	if err := sessionstore.CheckExecutionEnvironment(path, c.WorkspaceRoot()); err != nil {
		return err
	}
	_, bound, err := sessionstore.LoadExecutionEnvironment(path)
	if err != nil {
		return err
	}
	if !bound && c.rootEnvironment == nil {
		return nil
	}
	if c.rootEnvironment == nil {
		return sessionstore.ErrExecutionEnvironmentMismatch
	}
	return c.rootEnvironment.CheckSession(context.Background(), path)
}

func (c *Controller) adoptRootEnvironment(environment *rootenv.Environment) {
	if environment == nil {
		return
	}
	c.rootEnvironment = environment
	cleanup := c.cleanup
	c.cleanup = func() {
		defer environment.Close()
		if cleanup != nil {
			cleanup()
		}
	}
}
