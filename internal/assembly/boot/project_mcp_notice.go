package boot

import (
	"fmt"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/mcpsetup"
)

// reportProjectMCPAwaitingApproval says, once per build, which project servers
// are held off and how to let one run. Without it a headless run loses their
// tools with nothing on its diagnostic stream to say why.
func reportProjectMCPAwaitingApproval(sink event.Sink, cfg *config.Config, root string) {
	if cfg == nil {
		return
	}
	store := config.DefaultActivationStore()
	for _, entry := range cfg.Plugins {
		if !config.RepositoryDeclared(entry) {
			continue
		}
		var code, why string
		switch {
		case store.ServerChanged(entry, root):
			code, why = event.NoticeCodeProjectMCPChanged, "changed since you enabled it"
		case entry.ShouldAutoStart() && store.AwaitingDecision(entry, root):
			code, why = event.NoticeCodeProjectMCPAwaitingApproval, "is declared by this project and off until you approve it"
		default:
			continue
		}
		report(sink, event.Event{
			Level: event.LevelWarn,
			Code:  code,
			Text: fmt.Sprintf("MCP server %q %s; run `reasonix mcp enable %s` to approve the command shown.",
				mcpsetup.DisplayName(entry.Name), why, mcpsetup.DisplayName(entry.Name)),
			Detail: "command: " + mcpsetup.LaunchLine(entry),
		})
	}
}
