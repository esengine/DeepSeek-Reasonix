package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

const dormantRulesShown = 3

// reportDormantPermissionRules says which configured rules name no tool and so
// match nothing. Rules in an MCP or extension namespace are not judged: the
// server that supplies the tool may simply not be connected yet.
func reportDormantPermissionRules(sink event.Sink, cfg *config.Config, ctrl *control.Controller) {
	if cfg == nil || ctrl == nil {
		return
	}
	dormant := control.DormantRules(ctrl.PermissionVocabulary(), control.PermissionLists{
		Allow: cfg.Permissions.Allow, Ask: cfg.Permissions.Ask, Deny: cfg.Permissions.Deny,
	})
	if len(dormant) == 0 {
		return
	}
	shown := make([]string, 0, dormantRulesShown)
	for _, r := range dormant[:min(len(dormant), dormantRulesShown)] {
		shown = append(shown, fmt.Sprintf("%s %q", r.List, r.Rule))
	}
	more := ""
	if len(dormant) > dormantRulesShown {
		more = fmt.Sprintf(" and %d more", len(dormant)-dormantRulesShown)
	}
	report(sink, event.Event{
		Level: event.LevelWarn,
		Code:  event.NoticeCodePermissionRulesDormant,
		Text: fmt.Sprintf("Permission rules that name no tool match nothing, so they do not protect anything: %s%s. A shell command is written Bash(command:*).",
			strings.Join(shown, "; "), more),
		Detail: event.PermissionRulesDormant{Rules: dormant}.Encode(),
	})
}
