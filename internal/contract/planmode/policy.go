package planmode

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Marker is the model-facing plan-mode instruction block. It rides in the user
// turn, not the system prompt or tool schema, so plan toggles preserve cache shape.
const Marker = "[Plan mode — planning workflow. Gather context, ask clarifying questions with ask, maintain planning state with todo_write, and delegate focused research when useful. Do not begin implementation in this mode: until the user approves your plan the host refuses file writes, side-effecting shell commands, capability installation, memory mutation, writer-capable delegation, long-lived process control, and execution-step completion — don't spend turns retrying them. That refusal is a workflow phase, not a permission decision: approval switches the phase, and every call then goes through the active Permissions and Sandbox policy as usual. Reading, searching, inspecting, read-only shell commands, read-only delegation, todo_write, and ask stay available throughout. Before planning, when two or more genuinely reasonable options would change the architecture, public API, data model, dependencies, UX, compatibility, scope, or anything irreversible, that choice is the user's: ask first with the ask tool, recommended option first — having a recommendation does not make the choice yours. Purely local, low-risk, reversible details take the sensible default and enter the plan as a stated assumption rather than a question. Then present a LAYERED plan as your reply and stop. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve the plan before the workflow switches to implementation.]"

// Superseded lists Marker's earlier wordings. A session recorded under one of
// them still has to render as what the user typed, and the frontends that strip
// the prefix cannot know a text they were never compiled against.
var Superseded = []string{
	"[Plan mode — planning workflow. Gather context, ask clarifying questions with ask, maintain planning state with todo_write, and delegate focused research when useful. Do not begin implementation in this mode: until the user approves your plan the host refuses file writes, side-effecting shell commands, capability installation, memory mutation, writer-capable delegation, long-lived process control, and execution-step completion — don't spend turns retrying them. That refusal is a workflow phase, not a permission decision: approval switches the phase, and every call then goes through the active Permissions and Sandbox policy as usual. Reading, searching, inspecting, read-only shell commands, read-only delegation, todo_write, and ask stay available throughout. Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. Then present a LAYERED plan as your reply and stop. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve the plan before the workflow switches to implementation.]",
	"[Plan mode — planning workflow. Gather context, ask clarifying questions with ask, maintain planning state with todo_write, and delegate focused research when useful. Do not begin implementation in this mode: avoid file writes, unsafe shell commands, capability installation, memory mutation, writer-capable delegation, long-lived process control, or execution-step completion. This is a workflow instruction, not a permission boundary; every tool call remains governed by the active Permissions and Sandbox policy. Before planning, when two or more genuinely reasonable options would change the architecture, public API, data model, dependencies, UX, compatibility, scope, or anything irreversible, that choice is the user's: ask first with the ask tool, recommended option first — having a recommendation does not make the choice yours. Purely local, low-risk, reversible details take the sensible default and enter the plan as a stated assumption rather than a question. Then present a LAYERED plan as your reply and stop. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve the plan before the workflow switches to implementation.]",
	"[Plan mode — read-only. Explore the codebase first (read_file, ls, grep, glob, web_fetch, task, ask are available; writers are refused by the harness). Before planning, when two or more genuinely reasonable options would change the architecture, public API, data model, dependencies, UX, compatibility, scope, or anything irreversible, that choice is the user's: ask first with the ask tool, recommended option first — having a recommendation does not make the choice yours. Purely local, low-risk, reversible details take the sensible default and enter the plan as a stated assumption rather than a question. Then present a LAYERED plan as your reply and stop — do not write files, edit, or run side-effecting bash. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve before any changes are made.]",
}

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

// Effect is the caller's structural verdict on whether a call can change state
// outside the session. planmode is a leaf package: it decides the phase, it does
// not classify — callers pass what their own tool metadata and shell AST proved.
type Effect int

const (
	// EffectUnclassified is the zero value: the caller did not answer. Planning
	// reads it as a side effect, because a phase barrier that fails open is the
	// bug it exists to prevent.
	EffectUnclassified Effect = iota
	// EffectNone is a call the caller proved changes nothing outside the session.
	EffectNone
	// EffectSideEffect is a call that changes such state, or that the caller
	// could not prove does not.
	EffectSideEffect
)

// Call is the plan-mode view of one tool invocation. Effect decides the phase
// barrier; ReadOnly and Args remain for source compatibility with older callers
// and do not, because a bare bool cannot carry "not proven either way".
type Call struct {
	Name     string
	ReadOnly bool
	Safety   PlanSafety
	Effect   Effect
	Args     json.RawMessage
}

// BlockReason identifies which rule refused a call. Telemetry has to keep them
// apart: unclassified is classifier debt the host can go close, while the other
// two are the barrier doing exactly what it exists for.
type BlockReason string

const (
	BlockPhaseOptOut  BlockReason = "phase_opt_out"
	BlockSideEffect   BlockReason = "side_effect"
	BlockUnclassified BlockReason = "unclassified_effect"
)

// Decision reports whether phase semantics refuse a call and why.
type Decision struct {
	Blocked bool
	Reason  BlockReason
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

// Decide applies phase semantics only. A refusal here says the run is still
// planning, never that the caller lacks permission — but the phase's defining
// promise, no externally visible side effect before the user approves a plan, is
// Decide's to keep: Permissions modes are configured for the execution phase and
// would let a writer through while planning.
func (Policy) Decide(call Call) Decision {
	if call.Safety != PlanSafetyUnsafe {
		if call.Safety == PlanSafetySafe || call.Effect == EffectNone {
			return Decision{}
		}
		reason := BlockSideEffect
		if call.Effect == EffectUnclassified {
			reason = BlockUnclassified
		}
		return Decision{
			Blocked: true,
			Reason:  reason,
			Message: fmt.Sprintf("blocked: Plan mode is still planning, and %q would change state outside this session. "+
				"This is a workflow phase, not a permission decision: present the plan, and the user's approval "+
				"switches to execution, where this call goes through the ordinary Permissions and Sandbox path. "+
				"While planning, read, search, inspect, and ask run normally.", strings.TrimSpace(call.Name)),
		}
	}
	name := strings.TrimSpace(call.Name)
	if name == "complete_step" {
		return Decision{
			Blocked: true,
			Reason:  BlockPhaseOptOut,
			Message: "blocked: complete_step is only available after plan approval. While planning, keep task state with todo_write and present the plan for user approval.",
		}
	}
	return Decision{
		Blocked: true,
		Reason:  BlockPhaseOptOut,
		Message: fmt.Sprintf("blocked: %q is not available during the planning workflow. Finish or exit Plan mode before calling it.", name),
	}
}
