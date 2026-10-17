package control

import (
	"context"
	"errors"
	"fmt"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/agentpreset"
	"slices"
)

var errControllerClosed = errors.New("controller closed")

func compositionAllowsTool(binding *agentcomposition.Binding, name string) bool {
	return binding.Resolved.Tools.Mode == agentcomposition.Inherit || slices.ContainsFunc(binding.Resolved.Tools.Members, func(m agentcomposition.Module) bool { return m.ID == name })
}

type compositionControl struct {
	home         string
	selectPreset func(context.Context, string) error
	newSession   func(context.Context) error
	unavailable  error
	validate     func() error
}

func (c *Controller) ConfigureCompositions(home string, selectPreset func(context.Context, string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.composition == nil {
		c.composition = &compositionControl{}
	}
	c.composition.home, c.composition.selectPreset = home, selectPreset
}

func (c *Controller) PresetStore() *agentpreset.Store {
	c.mu.Lock()
	home := config.ReasonixHomeDir()
	if c.composition != nil && c.composition.home != "" {
		home = c.composition.home
	}
	c.mu.Unlock()
	return agentpreset.New(home, c.WorkspaceRoot())
}

func (c *Controller) AgentPresets() (agentpreset.Catalog, error) {
	if _, err := c.migrateLegacyPersona(); err != nil {
		return agentpreset.Catalog{}, err
	}
	catalog, err := c.PresetStore().List()
	if err != nil {
		return catalog, err
	}
	catalog.Modules, err = c.compositionInventory()
	return catalog, err
}
func (c *Controller) SaveAgentPreset(d agentcomposition.Definition, expected string) (agentcomposition.Version, error) {
	return c.PresetStore().Save(d, expected)
}
func (c *Controller) DeleteAgentPreset(id, expected string) error {
	return c.PresetStore().Delete(id, expected)
}
func (c *Controller) SetDefaultAgentPreset(id string, workspace bool) error {
	return c.PresetStore().SetDefault(id, workspace)
}

func (c *Controller) SelectAgentComposition(ctx context.Context, id string) error {
	if current := c.CompositionBinding(); current != nil && current.Version.Definition.ID == id {
		if v, err := c.PresetStore().Get(id); err == nil && v.Revision == current.Version.Revision {
			return nil
		}
	}
	release, err := c.BeginCompositionSwitch()
	if err != nil {
		return err
	}
	defer release()
	c.mu.Lock()
	var selectPreset func(context.Context, string) error
	if c.composition != nil {
		selectPreset = c.composition.selectPreset
	}
	c.mu.Unlock()
	if selectPreset == nil {
		return agentcomposition.ErrUnavailable
	}
	return selectPreset(ctx, id)
}

func (c *Controller) CompositionBinding() *agentcomposition.Binding {
	if c.executor == nil {
		return nil
	}
	return c.executor.Session().Composition()
}

func (c *Controller) BindComposition(binding *agentcomposition.Binding) {
	if c.executor != nil {
		c.executor.Session().BindComposition(binding)
	}
}

func (c *Controller) ConfigureCompositionNew(fn func(context.Context) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.composition == nil {
		c.composition = &compositionControl{}
	}
	c.composition.newSession = fn
}

func (c *Controller) newCompositionSession() error {
	release, err := c.BeginRuntimeReplacement()
	if err != nil {
		return err
	}
	defer release()
	c.mu.Lock()
	var create func(context.Context) error
	if c.composition != nil {
		create = c.composition.newSession
	}
	c.mu.Unlock()
	if create == nil {
		return fmt.Errorf("%w: create a new session through the owning frontend", agentcomposition.ErrUnavailable)
	}
	return create(context.Background())
}

// BeginCompositionSwitch holds turn admission until the replacement publishes.
func (c *Controller) BeginCompositionSwitch() (func(), error) {
	release, err := c.BeginRuntimeReplacement()
	if err != nil {
		return nil, err
	}
	if b := c.CompositionBinding(); b != nil && b.Started || c.Turn() != 0 || c.hasUnfinishedSessionJobs(c.SessionPath()) || c.PendingPrompt() {
		release()
		return nil, agentcomposition.ErrLocked
	}
	return release, nil
}

func (c *Controller) BeginRuntimeReplacement() (func(), error) {
	c.compositionMutationMu.Lock()
	if err := c.beginRotation(); err != nil {
		c.compositionMutationMu.Unlock()
		return nil, fmt.Errorf("%w: %w", agentcomposition.ErrLocked, err)
	}
	if c.hasUnfinishedSessionJobs(c.SessionPath()) || c.PendingPrompt() || len(c.InboxSnapshot().Items) > 0 {
		c.endRotation()
		c.compositionMutationMu.Unlock()
		return nil, agentcomposition.ErrLocked
	}
	return func() { c.endRotation(); c.compositionMutationMu.Unlock() }, nil
}

func (c *Controller) compositionEnqueueReady() error {
	c.mu.Lock()
	closed := c.gate.closed
	c.mu.Unlock()
	if closed {
		return errControllerClosed
	}
	return c.ensureWriteAuthorityReady()
}

func (c *Controller) persistCompositionAdmission() error {
	old := c.CompositionBinding()
	if old == nil || old.Started {
		return nil
	}
	binding := agentcomposition.Clone(old)
	binding.Started = true
	c.BindComposition(binding)
	c.EnsureSessionPath()
	if c.SessionPath() == "" {
		return nil
	}
	if err := c.Snapshot(); err != nil {
		c.BindComposition(old)
		return errors.Join(err, c.SnapshotRewrite())
	}
	return nil
}

func (c *Controller) beginCompositionEnqueue() (func() error, error) {
	old := c.CompositionBinding()
	noop := func() error { return nil }
	if old == nil || old.Started {
		return noop, nil
	}
	binding := agentcomposition.Clone(old)
	binding.Started = true
	if err := c.persistCompositionAdmission(); err != nil {
		return nil, err
	}
	return func() error {
		if c.Turn() != 0 || c.Running() {
			return nil
		}
		c.BindComposition(old)
		if err := c.Snapshot(); err != nil {
			c.BindComposition(binding)
			return err
		}
		return nil
	}, nil
}

func (c *Controller) compositionNeedsSnapshot(explicit bool) bool {
	b := c.CompositionBinding()
	return b != nil && (explicit || b.Started)
}

func (c *Controller) BlockCompositionExecution(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.composition == nil {
		c.composition = &compositionControl{}
	}
	c.composition.unavailable = err
}

func (c *Controller) CompositionExecutionError() error {
	c.mu.Lock()
	if c.composition == nil {
		c.mu.Unlock()
		return nil
	}
	err, validate := c.composition.unavailable, c.composition.validate
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if validate != nil {
		return validate()
	}
	return nil
}

func (c *Controller) ConfigureCompositionValidation(validate func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.composition == nil {
		c.composition = &compositionControl{}
	}
	c.composition.validate = validate
}
