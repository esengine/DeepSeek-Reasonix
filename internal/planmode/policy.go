package planmode

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Marker is the model-facing plan-mode instruction block. It rides in the user
// turn, not the system prompt or tool schema, so plan toggles preserve cache shape.
const Marker = "[Plan mode — planning workflow. Gather context, ask clarifying questions with ask, maintain planning state with todo_write, and delegate focused research when useful. Do not begin implementation in this mode: avoid file writes, unsafe shell commands, capability installation, memory mutation, writer-capable delegation, long-lived process control, or execution-step completion. This is a workflow instruction, not a permission boundary; every tool call remains governed by the active Permissions and Sandbox policy. Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. Then present a LAYERED plan as your reply and stop. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve the plan before the workflow switches to implementation.]"

// PlanSafety is a tool's explicit stance on whether the action belongs in the
// planning phase. It is deliberately not a write-safety classification: ordinary
// readers and writers both continue to Permissions/Sandbox.
type PlanSafety int

const (
	// PlanSafetyUnknown is the default. The call continues to Permissions/Sandbox.
	PlanSafetyUnknown PlanSafety = iota
	// PlanSafetySafe explicitly confirms that the call makes sense while planning.
	PlanSafetySafe
	// PlanSafetyUnsafe opts a tool out of the planning phase even when it is
	// side-effect-free. complete_step is the canonical example.
	PlanSafetyUnsafe
)

// Call is the plan-mode view of one tool invocation. ReadOnly,
// UntrustedReadOnly, and Args remain for source compatibility with older callers;
// they do not decide phase availability because Permissions/Sandbox own safety.
type Call struct {
	Name              string
	ReadOnly          bool
	UntrustedReadOnly bool
	Safety            PlanSafety
	Args              json.RawMessage
}

// Decision reports whether phase semantics refuse a call and why.
type Decision struct {
	Blocked bool
	Message string
}

// ReadOnlyCommandTrust is retained for source compatibility with the legacy
// Plan bash trust bridge. Decide no longer produces this request: bash safety is
// classified by Permissions, and read-only subagents enforce their own runner
// boundary directly.
type ReadOnlyCommandTrust struct {
	Command string
	Prefix  string
}

// Policy is retained so existing config/assembly code can carry legacy
// plan_mode_* fields without breaking old data. Those fields no longer grant or
// revoke execution in the main Plan workflow.
type Policy struct {
	AllowedTools     []string
	ReadOnlyCommands []string
}

// Decide applies phase semantics only. Plan is a collaboration workflow, not a
// security boundary: every ordinary call proceeds to the same permission and
// sandbox gates used outside Plan. A tool may explicitly opt out when executing
// it during planning is semantically invalid.
func (Policy) Decide(call Call) Decision {
	if call.Safety != PlanSafetyUnsafe {
		return Decision{}
	}
	name := strings.TrimSpace(call.Name)
	if name == "complete_step" {
		return Decision{
			Blocked: true,
			Message: "blocked: complete_step is only available after plan approval. While planning, keep task state with todo_write and present the plan for user approval.",
		}
	}
	return Decision{
		Blocked: true,
		Message: fmt.Sprintf("blocked: %q is not available during the planning workflow. Finish or exit Plan mode before calling it.", name),
	}
}
