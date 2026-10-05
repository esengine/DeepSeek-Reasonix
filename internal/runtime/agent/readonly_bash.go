package agent

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/permission"
)

type readOnlyBash struct {
	inner tool.Tool
}

func (b readOnlyBash) Name() string { return "bash" }

func (b readOnlyBash) Description() string {
	desc := strings.TrimSpace(b.inner.Description())
	if desc == "" {
		desc = "Execute a command in the shell and return combined stdout/stderr."
	}
	desc = strings.Replace(desc, "Execute a command in the shell", "Execute a foreground read-only command in the shell", 1)
	return desc + " Only permission-classified read-only commands are allowed; shell operators, background execution, process preservation, and write-capable arguments are blocked."
}

func (readOnlyBash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Read-only shell command to execute in the foreground. Must match the permission-layer read-only command policy."}},"required":["command"]}`)
}

func (b readOnlyBash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if !permission.BashCommandIsReadOnly(args) {
		return "", tool.Blocked(readOnlyBashBlockReason(bashCommandFromArgs(args)))
	}
	return b.inner.Execute(ctx, args)
}

// readOnlyBashBlockReason names the shape that disqualified the command, from
// the same classification the approval layer uses. A sub-agent told only that
// read-only commands are required rewrites whatever it guesses is at fault —
// three times, in one observed run.
func readOnlyBashBlockReason(command string) string {
	const lead = "blocked: a read-only sub-agent runs only commands the host classifies as read-only. "
	const remedy = " Prefer read_file, grep, ls, or glob for inspection; anything that writes belongs to the parent turn."
	switch permission.BashSubjectApprovalBlocker(command) {
	case permission.BashApprovalBlockerInlineCode:
		return lead + "This one carries inline interpreter code (python -c, node -e, bash -c), which the host cannot audit." + remedy
	case permission.BashApprovalBlockerNestedExecution:
		return lead + "This one nests another command through substitution ($(...), `...`, or <(...)), so what would run is not visible in it." + remedy
	case permission.BashApprovalBlockerDynamicName:
		return lead + "The program this one runs comes from a variable, so the host cannot tell what would execute." + remedy
	case permission.BashApprovalBlockerIndirectExecution:
		return lead + "This one runs through a wrapper (eval, source, xargs, find -exec) that executes something it does not name." + remedy
	case permission.BashApprovalBlockerHereDocBody:
		return lead + "This one feeds a here-document, whose body is file content rather than arguments the host can read." + remedy
	case permission.BashApprovalBlockerUnparsable:
		return lead + "Static analysis could not read this one — shell operators, loops, and redirections put it out of reach." + remedy
	default:
		return lead + "This one is outside the read-only command set, or carries an argument that can write." + remedy
	}
}

func (readOnlyBash) ReadOnly() bool { return true }
