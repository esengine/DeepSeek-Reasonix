package control

// controllerModelSettings keeps the immutable snapshot and its host admission
// callback together for the lifetime of one controller. The callback is guarded
// by Controller.mu; snapshot fields are immutable after construction.
type controllerModelSettings struct {
	effort              *string
	revision            string
	sourceRevision      string
	current             func() (string, error)
	beforeInboxDispatch func(*Controller) (func(), error)
}

func newControllerModelSettings(opts Options) controllerModelSettings {
	var effort *string
	if opts.FrozenEffort != nil {
		value := *opts.FrozenEffort
		effort = &value
	}
	return controllerModelSettings{
		effort:              effort,
		revision:            opts.ModelSettingsRevision,
		sourceRevision:      opts.ModelSettingsSourceRevision,
		current:             opts.ModelSettingsCurrent,
		beforeInboxDispatch: opts.BeforeInboxDispatch,
	}
}

// EffortSnapshot reports the current runtime's selection without reading
// mutable disk settings or a host's pending next-run selection.
func (c *Controller) EffortSnapshot() (string, bool) {
	if c.modelSettings.effort == nil {
		return "", false
	}
	return *c.modelSettings.effort, true
}
