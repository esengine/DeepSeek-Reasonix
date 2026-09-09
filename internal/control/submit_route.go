package control

import (
	"strings"

	"reasonix/internal/skill"
)

var managementNoticeCommands = map[string]struct{}{
	"/model": {}, "/provider": {}, "/memory": {}, "/migrate": {}, "/migration": {},
	"/skill": {}, "/skills": {}, "/plugin": {}, "/plugins": {}, "/reload-cmd": {},
	"/hooks": {}, "/mcp": {},
}

// SubmitDisposition describes whether a raw submit is expected to create a
// durable conversational turn. Management commands can still mutate session
// state or emit notices, but they must not be presented as turn admission.
type SubmitDisposition string

const (
	SubmitTurnStarted       SubmitDisposition = "turn_started"
	SubmitManagementHandled SubmitDisposition = "management_handled"
)

// ClassifySubmitRoute is the single controller-owned route classifier shared
// by desktop's turn receipt and the normal Submit path. Keep conditional
// commands here so a management-only form never gets mistaken for a turn.
func (c *Controller) ClassifySubmitRoute(input string) SubmitDisposition {
	trimmed := strings.TrimSpace(input)
	if isSimpleManagementInput(trimmed) {
		return SubmitManagementHandled
	}
	return c.classifySlashSubmit(trimmed)
}

func isSimpleManagementInput(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	if _, ok := MemoryQuickAddNote(trimmed); ok {
		return true
	}
	if _, ok := RememberCommandNote(trimmed); ok {
		return true
	}
	if goal, ok := ParseGoalCommand(trimmed); ok {
		return goal.Action != GoalCommandSet
	}
	if trimmed == "/reload" || trimmed == "/effort" || strings.HasPrefix(trimmed, "/effort ") {
		return true
	}
	switch trimmed {
	case "/compact", "/context", "/new", "/clear":
		return true
	default:
		return false
	}
}

func (c *Controller) classifySlashSubmit(trimmed string) SubmitDisposition {
	if _, ok := ParseFinalReadinessRecoveryCommand(trimmed); ok || !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "!") {
		return SubmitTurnStarted
	}
	if strings.HasPrefix(trimmed, "/mcp__") || c.isPathSubmit(trimmed) {
		return SubmitTurnStarted
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return SubmitTurnStarted
	}
	switch fields[0] {
	case "/tree", "/branch", "/switch", "/rewind":
		return SubmitManagementHandled
	case "/plan-exec":
		return c.classifyPlanExecSubmit()
	case "/prometheus":
		return classifyPrometheusSubmit(trimmed, fields[0])
	}
	if _, ok := managementNoticeCommands[fields[0]]; ok {
		return SubmitManagementHandled
	}
	if IsBuiltinDocsSlash(fields[0], c.Commands(), c.SlashSkills()) {
		return classifyDocsSubmit(trimmed, fields[0])
	}
	if sk, task, ok := c.resolveSkillInvocation(trimmed); ok && sk.RunAs == skill.RunSubagent && strings.TrimSpace(task) == "" {
		return SubmitManagementHandled
	}
	return SubmitTurnStarted
}

func (c *Controller) isPathSubmit(trimmed string) bool {
	if _, ok := FileRefLine(trimmed); ok {
		return true
	}
	if _, ok := SlashPathLineRef(trimmed, c.workspaceRoot); ok {
		return true
	}
	return SlashPathLikeLine(trimmed)
}

func (c *Controller) classifyPlanExecSubmit() SubmitDisposition {
	if c.executor == nil || len(c.executor.CanonicalTodoState()) == 0 {
		return SubmitManagementHandled
	}
	return SubmitTurnStarted
}

func classifyPrometheusSubmit(trimmed, command string) SubmitDisposition {
	args := strings.TrimSpace(strings.TrimPrefix(trimmed, command))
	if args == "" || args == "--strict" {
		return SubmitManagementHandled
	}
	if strings.HasPrefix(args, "--strict ") && strings.TrimSpace(strings.TrimPrefix(args, "--strict ")) == "" {
		return SubmitManagementHandled
	}
	return SubmitTurnStarted
}

func classifyDocsSubmit(trimmed, command string) SubmitDisposition {
	if strings.TrimSpace(strings.TrimPrefix(trimmed, command)) == "" {
		return SubmitManagementHandled
	}
	return SubmitTurnStarted
}

// SubmitResult is returned by the result-capable desktop submit path. The
// legacy Submit* methods intentionally keep their void/error contracts.
type SubmitResult struct {
	Disposition SubmitDisposition `json:"disposition"`
}

// SubmitDisplayWithResult preserves the existing asynchronous submit behavior
// while exposing the route disposition to hosts that need an admission receipt.
func (c *Controller) SubmitDisplayWithResult(display, input string) SubmitResult {
	disposition := c.ClassifySubmitRoute(input)
	c.SubmitDisplay(display, input)
	return SubmitResult{Disposition: disposition}
}
