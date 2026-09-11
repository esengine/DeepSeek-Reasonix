package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/permission"
)

type approvalChoice struct {
	label           string
	allow           bool
	allowForSession bool
	persistToConfig bool
	exitPlan        bool
}

func approvalChoices(a *event.Approval) []approvalChoice {
	if a == nil {
		return nil
	}
	fresh := a.Fresh || control.RequiresFreshHumanApprovalTool(a.Tool)
	var decisions []approvalChoice
	switch {
	case isRecoveryApprovalEvent(a):
		if a.Recovery != nil && a.Recovery.CanGrantTask {
			decisions = []approvalChoice{{allow: true}, {allow: true, allowForSession: true}, {}}
		} else {
			decisions = []approvalChoice{{allow: true}, {}}
		}
	case a.Tool == planApprovalTool:
		decisions = []approvalChoice{{allow: true}, {}, {exitPlan: true}}
	case a.Kind == event.ApprovalKindWriteAccess || a.WriteAccess != nil:
		decisions = []approvalChoice{{allow: true}, {allow: true, allowForSession: true}, {allow: true, allowForSession: true, persistToConfig: true}, {}}
	case fresh && freshApprovalAllowsSession(a.Tool):
		decisions = []approvalChoice{{allow: true}, {allow: true, allowForSession: true}, {}}
	case fresh:
		decisions = []approvalChoice{{allow: true}, {}}
	default:
		decisions = []approvalChoice{{allow: true}, {allow: true, allowForSession: true}, {allow: true, allowForSession: true, persistToConfig: true}, {}}
	}
	labels := approvalChoiceLabels(a)
	for i := range decisions {
		if i < len(labels) {
			decisions[i].label = labels[i]
		}
	}
	return decisions
}

func approvalChoiceLabels(a *event.Approval) []string {
	choices := i18n.M.FreshHumanApprovalChoices
	fresh := a.Fresh || control.RequiresFreshHumanApprovalTool(a.Tool)
	if isRecoveryApprovalEvent(a) {
		choices = i18n.M.RecoveryApprovalChoices
		if isRecoveryPlanChangeApproval(a) {
			choices = i18n.M.RecoveryPlanChangeChoices
		} else if a.Recovery != nil && a.Recovery.CanGrantTask {
			choices = i18n.M.RecoveryTaskGrantChoices
		}
	} else if a.Tool == planApprovalTool {
		choices = i18n.M.PlanApprovalChoices
	} else if !fresh {
		sessionRule := permission.SessionGrantRuleForScope(a.Tool, a.Subject)
		persistentRule := permission.RememberRuleForScope(a.Tool, a.Subject)
		choices = fmt.Sprintf(i18n.M.ToolApprovalChoices, sessionRule, persistentRule)
	}
	switch a.Tool {
	case control.SandboxEscapeApprovalTool:
		choices = i18n.M.SandboxEscapeApprovalChoices
	case control.ManagedConfigWriteApprovalTool:
		choices = i18n.M.ConfigWriteApprovalChoices
	case agent.PlanModeReadOnlyCommandApprovalTool:
		choices = i18n.M.PlanModeReadOnlyCommandChoices
	}
	if a.Kind == event.ApprovalKindWriteAccess || a.WriteAccess != nil {
		choices = i18n.M.WriteAccessApprovalChoices
	}
	if !fresh && a.Tool == "bash" && permission.BashCommandPrefix(a.Subject) != "" {
		rule := permission.RememberRuleForScope(a.Tool, a.Subject)
		choices = fmt.Sprintf(i18n.M.BashPrefixChoices, rule, rule)
	}
	var labels []string
	for line := range strings.SplitSeq(choices, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 3 && line[0] >= '1' && line[0] <= '9' && line[1] == '.' {
			labels = append(labels, strings.TrimSpace(line[2:]))
		}
	}
	if isRecoveryApprovalEvent(a) && a.Recovery != nil && a.Recovery.CanGrantTask && len(labels) > 1 {
		if scope := strings.TrimSpace(a.Recovery.TaskGrantScope); scope != "" {
			labels[1] += " — " + scope
		}
	}
	return labels
}

func writeAccessBannerDetails(a *event.Approval) []string {
	if a == nil || a.WriteAccess == nil {
		return nil
	}
	wa := a.WriteAccess
	dirs := wa.DisplayDirectories
	if len(dirs) == 0 {
		dirs = wa.Directories
	}
	details := make([]string, 0, 4)
	if len(dirs) > 0 {
		details = append(details, strings.Join(dirs, ", "))
	}
	if wa.BroadHomeAccess {
		details = append(details, i18n.M.WriteAccessHomeWarning)
	}
	if wa.OrdinaryPermissionNeeded {
		details = append(details, i18n.M.WriteAccessMergedPermissionHint)
	}
	if wa.PersistAllowed {
		details = append(details, i18n.M.WriteAccessProjectHint)
	}
	return details
}
