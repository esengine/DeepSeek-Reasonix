package control

import (
	"context"
	"errors"

	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/state/sessionstore"
)

// RootEnvironmentPolicy captures host environment operations for one runtime
// generation. Preparation requires a reserved runtime replacement.
type RootEnvironmentPolicy struct {
	Generation uint64
	Block      func() error
	Prepare    func(context.Context, string) (*rootenv.Environment, *config.Config, error)
	Publish    func(context.Context, *Controller) error
	Recheck    func(context.Context, *Controller) error
	Retry      func(context.Context, *Controller, string) error
	Recover    func(context.Context, *Controller, string) error
	Results    func(context.Context, *Controller) (isolation.Catalog, error)
}

func (c *Controller) RootEnvironmentStrategy() string {
	if c.coordination == nil {
		return "legacy"
	}
	return c.coordination.Collaboration
}

// RootEnvironmentSelectionPending is a hint for hosts before reserving a fresh
// root. Preparation repeats the authoritative check under the replacement gate.
func (c *Controller) RootEnvironmentSelectionPending() bool {
	c.mu.Lock()
	started := c.startedOnce
	c.mu.Unlock()
	return !started && c.rootEnvironment == nil && c.executor != nil && c.executor.Session().Len() <= 1
}

func (c *Controller) CheckRootEnvironmentPreparation() error {
	c.mu.Lock()
	blocked := c.gate.closed || c.gate.running || c.gate.finishing || !c.gate.rotating || c.startedOnce
	generation := c.runtimeGeneration
	c.mu.Unlock()
	if blocked {
		return errors.New("root environment selection requires a fresh runtime reserved for replacement")
	}
	policy := c.rootEnvironmentPolicy
	if c.rootEnvironment != nil || c.observe != nil || policy == nil || policy.Prepare == nil || policy.Generation != generation {
		return errors.New("this runtime cannot move to a new root environment")
	}
	if c.executor == nil || c.executor.Session() == nil || c.executor.Session().Len() > 1 {
		return errors.New("an existing conversation cannot move automatically")
	}
	if c.jobs != nil && c.jobs.HasUnfinishedForSession("") {
		return ErrJobsRunning
	}
	if policy.Block != nil {
		if err := policy.Block(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) PrepareRootEnvironment(ctx context.Context) (*rootenv.Environment, *config.Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := c.CheckRootEnvironmentPreparation(); err != nil {
		return nil, nil, err
	}
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return nil, nil, err
	}
	path := c.SessionPath()
	if path == "" || !c.executor.Session().WriteAuthority().Covers(path) {
		return nil, nil, sessionstore.ErrSessionWriteAuthorityMissing
	}
	return c.rootEnvironmentPolicy.Prepare(ctx, path)
}

func (c *Controller) BindPreparedRootEnvironment(ctx context.Context, environment *rootenv.Environment) error {
	if err := c.CheckRootEnvironmentPreparation(); err != nil {
		return err
	}
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return err
	}
	return environment.BindSession(ctx, c.SessionPath(), c.executor.Session().WriteAuthority())
}
