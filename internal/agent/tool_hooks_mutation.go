package agent

type toolMutationHookReporter interface {
	ToolMutationHooksEnabled() bool
}

func toolHooksMayMutateWorkspace(hooks ToolHooks) bool {
	if hooks == nil {
		return false
	}
	if reporter, ok := hooks.(toolMutationHookReporter); ok {
		return reporter.ToolMutationHooksEnabled()
	}
	// Custom ToolHooks implementations predate the capability report. Preserve
	// conservative coverage because their callbacks may write files.
	return true
}
