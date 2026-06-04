package codegraph

// ReadOnlyToolNames returns the CodeGraph MCP tools Reasonix treats as readers
// when older CodeGraph runtimes omit MCP annotations.readOnlyHint metadata.
func ReadOnlyToolNames() map[string]bool {
	return map[string]bool{
		"codegraph_callees": true,
		"codegraph_callers": true,
		"codegraph_context": true,
		"codegraph_explore": true,
		"codegraph_files":   true,
		"codegraph_impact":  true,
		"codegraph_node":    true,
		"codegraph_search":  true,
		"codegraph_status":  true,
		"codegraph_trace":   true,
	}
}
