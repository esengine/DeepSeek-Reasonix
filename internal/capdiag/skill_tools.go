package capdiag

import (
	"reasonix/internal/config"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin" // Initialize compile-time tool identities.
)

func skillToolIssues(store *skill.Store, cfg *config.Config, mcp MCPReport, sanitize func(string) string) []Issue {
	var issues []Issue
	opts := skill.ToolReferenceOptions{Known: tool.KnownToolNames(), Bindings: mcp.bindings}
	failed := map[string]string{}
	for _, server := range mcp.Servers {
		if server.RuntimeStatus == "failed" {
			failed[server.Name] = server.Error
		}
	}
	diagnostics := skill.CheckToolReferences(store.List(), opts)
	diagnostics = append(diagnostics, skill.CheckMCPRequirements(store.List(), cfg.Plugins, failed)...)
	for _, d := range diagnostics {
		issues = append(issues, Issue{
			Severity: d.Severity, Code: d.Code, Subsystem: "skills", Name: d.Skill,
			Message: sanitize(d.Message), SettingsTab: "skills",
			Remediation: "Check the reference against the target session's tool inventory; offline recognition does not establish runtime availability",
		})
	}
	return issues
}
