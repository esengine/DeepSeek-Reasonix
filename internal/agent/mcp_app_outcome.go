package agent

import (
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// toProviderMCPApp lifts the collected Apps presentation onto the outcome.
func toProviderMCPApp(r *tool.MCPAppResult) *provider.MCPAppPresentation {
	if r == nil {
		return nil
	}
	bounded := r.Sanitized()
	if bounded == nil {
		return nil
	}
	return &provider.MCPAppPresentation{
		Server: bounded.Server, Tool: bounded.Tool, Generation: bounded.Generation,
		ResourceURI: bounded.ResourceURI, CSP: bounded.CSP,
		RawResult: bounded.RawResult, Structured: bounded.Structured,
	}
}
