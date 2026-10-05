package evidence

import (
	"encoding/json"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/shellsafe"
)

// Mutation classes say how well the host knows a call changed state. The
// distinction the boolean cannot carry: "I cannot prove this is read-only" and
// "this wrote something I cannot name" are different claims, and only the
// second one describes a change set a reviewer could be pointed at.
const (
	MutationNone    = "none"
	MutationProven  = "proven"
	MutationUnknown = "unknown"
)

// ToolCallMutationClass classifies how certainly a call changes state.
// ToolCallMutates is its conservative boolean view: anything but MutationNone
// counts, so permission and delivery floors keep treating unknown as a write.
func ToolCallMutationClass(toolName string, args json.RawMessage, readOnly bool) string {
	if readOnly || IsNonMutationMetaTool(toolName) {
		return MutationNone
	}
	switch toolName {
	case "ask", "todo_write", "complete_step", "bash_output", "wait":
		return MutationNone
	case "browser_open", "browser_act":
		// Their effects land on the web: the browser downloads nothing and has no
		// way to upload, so no call of theirs writes a workspace path.
		return MutationNone
	case "computer_read":
		return MutationNone
	case "computer_act":
		// Another application can write anywhere it may, the workspace included,
		// and reports no paths.
		return MutationUnknown
	case "bash":
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(args, &fields); err != nil {
			return MutationUnknown
		}
		return bashMutationClass(stringField(fields, "command"))
	default:
		// A writer tool's contract is the write, and it reports the paths it
		// touched; an opaque tool call names no path but still ran on purpose.
		return MutationProven
	}
}

// ToolCallMutationClassWith is ToolCallMutationClass for a call whose tool is
// known: a tool with unstated effects is unknown rather than proven, so the
// workspace it ran against says whether it changed anything.
func ToolCallMutationClassWith(toolName string, args json.RawMessage, facts ToolFacts) string {
	class := ToolCallMutationClass(toolName, args, facts.ReadOnly)
	if class == MutationProven && facts.EffectsUnstated {
		return MutationUnknown
	}
	return class
}

func bashMutationClass(command string) string {
	class, _ := bashScanMutation(command)
	return class
}

// bashScanMutation walks a command's top-level segments once, returning the
// strongest class any of them carries and the first segment that was not
// provably clean — the one a block message should quote back.
func bashScanMutation(command string) (class, unproven string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return MutationUnknown, ""
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) == 0 {
		return MutationUnknown, ""
	}
	class = MutationNone
	for _, segment := range segments {
		switch bashSegmentMutationClass(segment) {
		case MutationProven:
			if unproven == "" {
				unproven = segment
			}
			return MutationProven, unproven
		case MutationUnknown:
			class = MutationUnknown
			if unproven == "" {
				unproven = segment
			}
		}
	}
	return class, unproven
}

func bashSegmentMutationClass(segment string) string {
	if writes, decided := shellsafe.BashRedirectsToNamedFile(segment); decided && writes {
		return MutationProven
	}
	normalized, safeRedirects := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
	if !safeRedirects {
		return MutationUnknown
	}
	if fields, malformed := shellparse.StaticFields(normalized); malformed == "" && len(fields) > 0 && bashSegmentIsVerification(fields) {
		return MutationNone
	}
	base, sub, fields, workspaceNonMutating := shellsafe.ClassifyWorkspaceNonMutatingCommand(normalized)
	if !workspaceNonMutating {
		return MutationUnknown
	}
	if shellsafe.ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
		return MutationProven
	}
	return MutationNone
}
