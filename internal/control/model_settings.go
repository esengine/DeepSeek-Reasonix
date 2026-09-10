package control

// controllerModelSettings keeps the immutable snapshot and its host admission
// callback together for the lifetime of one controller. The callback is guarded
// by Controller.mu; snapshot fields are immutable after construction.
type controllerModelSettings struct {
	revision            string
	sourceRevision      string
	current             func() (string, error)
	beforeInboxDispatch func(*Controller) (func(), error)
}

func newControllerModelSettings(opts Options) controllerModelSettings {
	return controllerModelSettings{
		revision:            opts.ModelSettingsRevision,
		sourceRevision:      opts.ModelSettingsSourceRevision,
		current:             opts.ModelSettingsCurrent,
		beforeInboxDispatch: opts.BeforeInboxDispatch,
	}
}
