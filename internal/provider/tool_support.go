package provider

// ToolSupportProvider selects the pure-text compatibility path when false.
// Providers default to tool-capable for backward compatibility.
type ToolSupportProvider interface {
	SupportsTools() bool
}

func SupportsTools(p Provider) bool {
	capable, ok := p.(ToolSupportProvider)
	return !ok || capable.SupportsTools()
}
