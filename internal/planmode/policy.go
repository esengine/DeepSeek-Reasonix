package planmode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Marker is the model-facing plan-mode instruction block. It rides in the user
// turn, not the system prompt or tool schema, so plan toggles preserve cache shape.
const Marker = "[Plan mode — planning only. You may research the codebase and web, ask clarifying questions with ask, maintain planning state with todo_write, and delegate isolated read-only research with read_only_task or read_only_skill. You must not write files, run unsafe shell commands, install capabilities, mutate memory, delegate to writer-capable sub-agents or skills, control long-lived processes, or mark execution steps complete. Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. Then present a LAYERED plan as your reply and stop. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve before any changes are made.]"

// PlanSafety is a tool's self-reported stance on running during the planning
// phase, surfaced via tool.PlanModeClassifier. It is deliberately distinct from
// ReadOnly(): a tool can be side-effect-free (ReadOnly) yet still belong only to
// the post-approval execution phase — complete_step is the canonical example.
type PlanSafety int

const (
	// PlanSafetyUnknown means the tool does not implement PlanModeClassifier, so
	// the policy falls back to its audited read-only whitelist.
	PlanSafetyUnknown PlanSafety = iota
	// PlanSafetySafe means the tool asserts it is safe to run while planning.
	PlanSafetySafe
	// PlanSafetyUnsafe means the tool asserts it must not run while planning,
	// even though ReadOnly() may be true.
	PlanSafetyUnsafe
)

// Call is the plan-mode view of one tool invocation.
type Call struct {
	Name     string
	ReadOnly bool
	// Untrusted is true when ReadOnly came from an external, untrusted source —
	// an MCP server's readOnlyHint. Plan mode does not take such a flag at face
	// value and gates the tool like a writer. Set by the agent from
	// tool.PlanModeUntrustedReadOnly at the gate call site.
	Untrusted bool
	// Safety is the tool's self-reported plan-mode stance. It is Unknown when
	// the tool does not implement tool.PlanModeClassifier; the agent translates
	// the interface result into this field at the gate call site.
	Safety PlanSafety
	Args   json.RawMessage
}

// Decision reports whether plan mode refuses a call and why.
type Decision struct {
	Blocked bool
	Message string
}

// Policy is the single plan-mode stage policy.
type Policy struct {
	AllowedTools []string
}

var knownBlockedTools = map[string]bool{
	"write_file":      true,
	"edit_file":       true,
	"multi_edit":      true,
	"move_file":       true,
	"apply_patch":     true,
	"edit_notebook":   true,
	"notebook_edit":   true,
	"range_delete":    true,
	"symbol_delete":   true,
	"delete_range":    true,
	"delete_symbol":   true,
	"complete_step":   true,
	"task":            true,
	"parallel_tasks":  true,
	"run_skill":       true,
	"explore":         true,
	"research":        true,
	"review":          true,
	"security_review": true,
	"security-review": true,
	"install_source":  true,
	"install_skill":   true,
	"remember":        true,
	"forget":          true,
	"kill_shell":      true,
}

var alwaysAllowedTools = map[string]bool{
	"ask":        true,
	"todo_write": true,
}

// planSafeReadOnly is the audited set of read-only built-in tools confirmed safe
// to run during planning. It is the AUDIT record, not Decide's allow path: Decide
// already trusts any in-process ReadOnly()==true tool. reconcile_test.go uses this
// map (via Classify) to force every built-in into an explicit bucket, so a newly
// added built-in cannot merge without a reviewer recording its plan-mode stance —
// here, in knownBlockedTools, or via tool.PlanModeClassifier.
var planSafeReadOnly = map[string]bool{
	"read_file":   true,
	"ls":          true,
	"glob":        true,
	"grep":        true,
	"code_index":  true,
	"web_fetch":   true,
	"bash_output": true, // observes an already-running job's buffered output; no new side effect
	"wait":        true, // observes job status; cannot start, preserve, or kill processes
}

var bashMetachars = []string{"&&", "||", ">>", "<<", "$(", "\x60", ";", "|", ">", "<", "&", "\n", "\r"}

var safeBashCommands = []string{
	"git status", "git diff", "git log", "git show",
	"git ls-files", "git grep", "git blame",
	"ls", "cat", "grep", "find", "head", "tail", "pwd",
	"echo", "wc", "which", "type", "uname", "hostname",
	"go version", "go list", "go doc", "go vet",
	"node -v", "npm list", "python --version",
}

var findWriteArgs = map[string]bool{
	"-delete":  true,
	"-exec":    true,
	"-execdir": true,
	"-ok":      true,
	"-okdir":   true,
	"-fprint":  true,
	"-fprintf": true,
	"-fls":     true,
}

var goWriteOrExecArgs = map[string]bool{
	"-fix":      true,
	"-mod":      true,
	"-modfile":  true,
	"-toolexec": true,
	"-vettool":  true,
}

// Decide applies the plan-mode stage gate before permission policy. The boundary
// is fail-closed for untrusted tools: a tool whose ReadOnly() is false, or whose
// ReadOnly() is asserted by an untrusted external source (an MCP server's
// readOnlyHint, surfaced via Call.Untrusted), is refused unless it self-reports
// plan-safe or is declared in plan_mode_allowed_tools. A trustworthy
// ReadOnly()==true tool — a built-in or a first-party MCP override — is allowed,
// EXCEPT one that self-reports PlanSafetyUnsafe (complete_step: read-only yet
// post-approval only), which is refused regardless. The invariant
// PlanSafe ⇒ ReadOnly is enforced: a writer that claims plan-safe is a wiring
// bug and is refused.
func (p Policy) Decide(call Call) Decision {
	name := strings.TrimSpace(call.Name)
	if name == "bash" {
		return decideBash(call.Args)
	}
	if knownBlockedTools[name] {
		return blockKnown(name)
	}
	if call.Safety == PlanSafetyUnsafe {
		return blockKnown(name)
	}
	if alwaysAllowedTools[name] {
		return Decision{}
	}
	if call.Safety == PlanSafetySafe {
		if !call.ReadOnly {
			return planSafeContractViolation(name)
		}
		return Decision{}
	}
	if call.ReadOnly && !call.Untrusted {
		// Trusted: built-ins and first-party MCP overrides report a trustworthy
		// ReadOnly()==true. A read-only tool that is nonetheless unsafe while
		// planning is caught above via PlanSafetyUnsafe / knownBlockedTools.
		return Decision{}
	}
	if p.allowed(name) {
		return Decision{}
	}
	if call.ReadOnly && call.Untrusted {
		return Decision{
			Blocked: true,
			Message: fmt.Sprintf("blocked: %q reports read-only, but that flag is self-reported by an untrusted external source (e.g. an MCP server's readOnlyHint) that plan mode does not trust. Declare it in plan_mode_allowed_tools to use it while planning.", name),
		}
	}
	return Decision{
		Blocked: true,
		Message: fmt.Sprintf("blocked: %q is a writer tool and plan mode is read-only. Keep exploring with read-only tools, then write your plan as your reply — the user will be asked to approve it before any changes are made.", name),
	}
}

// IgnoredAllowedTools names configured overrides that plan mode will not honor.
func (p Policy) IgnoredAllowedTools() []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range p.AllowedTools {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		if name == "bash" || knownBlockedTools[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	sort.Strings(out)
	return out
}

func (p Policy) allowed(name string) bool {
	for _, allowed := range p.AllowedTools {
		if strings.TrimSpace(allowed) == name {
			return true
		}
	}
	return false
}

func blockKnown(name string) Decision {
	if name == "complete_step" {
		return Decision{
			Blocked: true,
			Message: "blocked: complete_step is only available after plan approval. While planning, keep task state with todo_write and present the plan for user approval.",
		}
	}
	return Decision{
		Blocked: true,
		Message: fmt.Sprintf("blocked: %q is not available in plan mode. Keep exploring with read-only tools — the user will be asked to approve the plan before any changes are made.", name),
	}
}

func planSafeContractViolation(name string) Decision {
	return Decision{
		Blocked: true,
		Message: fmt.Sprintf("blocked: %q is classified plan-safe but reports ReadOnly()==false; refusing on the PlanSafe ⇒ ReadOnly invariant. This is a tool wiring bug — fix the tool's ReadOnly()/PlanModeSafe() contract.", name),
	}
}

// Class is the plan-mode bucket a tool falls into, independent of any
// plan_mode_allowed_tools override. It exists so reconcile_test.go and
// marker_test.go can assert that every built-in is *explicitly* classified
// rather than implicitly allowed. Branch order matches Decide; the override is
// excluded on purpose because it is a deployment-specific escape valve, not part
// of the built-in taxonomy.
type Class int

const (
	// ClassBashGated is bash, whose safety is decided per-argument in decideBash.
	ClassBashGated Class = iota
	// ClassBlockedKnown is a tool in knownBlockedTools.
	ClassBlockedKnown
	// ClassBlockedUnsafe is a tool that self-reports PlanSafetyUnsafe.
	ClassBlockedUnsafe
	// ClassAlwaysAllowed is ask / todo_write.
	ClassAlwaysAllowed
	// ClassPlanSafeSelfReported is a tool that self-reports PlanSafetySafe.
	ClassPlanSafeSelfReported
	// ClassPlanSafeAudited is a tool in the planSafeReadOnly whitelist.
	ClassPlanSafeAudited
	// ClassDefaultBlocked is the fail-closed bucket: nothing classified the tool
	// plan-safe, so plan mode refuses it.
	ClassDefaultBlocked
)

// Classify reports the plan-mode bucket for a tool. It mirrors Decide's
// classification (minus the override and the PlanSafe ⇒ ReadOnly invariant
// check, which callers assert separately): a plan-safe class still requires
// ReadOnly(), and reconcile_test.go enforces that.
func Classify(name string, readOnly bool, safety PlanSafety) Class {
	name = strings.TrimSpace(name)
	if name == "bash" {
		return ClassBashGated
	}
	if knownBlockedTools[name] {
		return ClassBlockedKnown
	}
	if safety == PlanSafetyUnsafe {
		return ClassBlockedUnsafe
	}
	if alwaysAllowedTools[name] {
		return ClassAlwaysAllowed
	}
	if safety == PlanSafetySafe {
		return ClassPlanSafeSelfReported
	}
	if planSafeReadOnly[name] {
		return ClassPlanSafeAudited
	}
	return ClassDefaultBlocked
}

func decideBash(args json.RawMessage) Decision {
	var p struct {
		Command                     string `json:"command"`
		RunInBackground             bool   `json:"run_in_background"`
		PreserveBackgroundProcesses bool   `json:"preserve_background_processes"`
	}
	if err := json.Unmarshal(args, &p); err != nil || strings.TrimSpace(p.Command) == "" {
		return Decision{
			Blocked: true,
			Message: "blocked: bash command in plan mode must include a valid read-only command.",
		}
	}
	if p.RunInBackground {
		return Decision{
			Blocked: true,
			Message: "blocked: bash background execution is not available in plan mode. Use foreground read-only commands while planning.",
		}
	}
	if p.PreserveBackgroundProcesses {
		return Decision{
			Blocked: true,
			Message: "blocked: bash process preservation is not available in plan mode. Use foreground read-only commands while planning.",
		}
	}
	cmd := strings.TrimSpace(p.Command)
	lower := strings.ToLower(cmd)

	for _, mc := range bashMetachars {
		if strings.Contains(lower, mc) {
			return Decision{
				Blocked: true,
				Message: fmt.Sprintf("blocked: bash command in plan mode must not contain shell operators (%q). Use separate calls for chained commands.", mc),
			}
		}
	}

	for _, safe := range safeBashCommands {
		if !bashMatchesSafePrefix(lower, safe) {
			continue
		}
		arg, err := unsafeSafeCommandArg(cmd, safe)
		if err != "" {
			return Decision{
				Blocked: true,
				Message: fmt.Sprintf("blocked: bash command in plan mode has malformed shell quoting (%s). Use a simple read-only command while planning.", err),
			}
		}
		if arg != "" {
			return Decision{
				Blocked: true,
				Message: fmt.Sprintf("blocked: bash command in plan mode uses a write-capable argument (%q). Use a read-only command while planning.", arg),
			}
		}
		return Decision{}
	}

	return Decision{
		Blocked: true,
		Message: fmt.Sprintf("blocked: bash commands in plan mode must be read-only. %q is not in the safe command list. Use read-only tools for exploration, then exit plan mode to run this command.", cmd),
	}
}

func bashMatchesSafePrefix(lower, safe string) bool {
	if !strings.HasPrefix(lower, safe) {
		return false
	}
	if len(lower) == len(safe) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(lower[len(safe):])
	return unicode.IsSpace(r)
}

func unsafeSafeCommandArg(cmd, safe string) (string, string) {
	fields, err := shellFields(cmd)
	if err != "" {
		return "", err
	}
	base := strings.Fields(safe)
	if len(fields) <= len(base) {
		return "", ""
	}
	args := fields[len(base):]
	lowerArgs := make([]string, len(args))
	for i, arg := range args {
		lowerArgs[i] = strings.ToLower(arg)
	}
	if strings.HasPrefix(safe, "git ") {
		for _, arg := range lowerArgs {
			if arg == "--output" || strings.HasPrefix(arg, "--output=") || arg == "--ext-diff" {
				return arg, ""
			}
		}
	}
	switch safe {
	case "git grep":
		for i, arg := range args {
			lowerArg := lowerArgs[i]
			if arg == "-O" || strings.HasPrefix(arg, "-O") || strings.HasPrefix(lowerArg, "--open-files-in-pager") {
				return arg, ""
			}
		}
	case "find":
		for _, arg := range lowerArgs {
			if findWriteArgs[arg] {
				return arg, ""
			}
		}
	case "go list", "go vet":
		for _, arg := range lowerArgs {
			if goWriteOrExecArgs[arg] || strings.HasPrefix(arg, "-mod=mod") || strings.HasPrefix(arg, "-modfile=") || strings.HasPrefix(arg, "-toolexec=") || strings.HasPrefix(arg, "-vettool=") {
				return arg, ""
			}
		}
	}
	return "", ""
}

func shellFields(s string) ([]string, string) {
	var fields []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	haveField := false
	flush := func() {
		if haveField {
			fields = append(fields, b.String())
			b.Reset()
			haveField = false
		}
	}
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			haveField = true
			escaped = false
			continue
		}
		if inSingle {
			if r == '\'' {
				inSingle = false
				continue
			}
			b.WriteRune(r)
			haveField = true
			continue
		}
		if inDouble {
			switch r {
			case '"':
				inDouble = false
			case '\\':
				escaped = true
			default:
				b.WriteRune(r)
				haveField = true
			}
			continue
		}
		switch {
		case unicode.IsSpace(r):
			flush()
		case r == '\'':
			inSingle = true
			haveField = true
		case r == '"':
			inDouble = true
			haveField = true
		case r == '\\':
			escaped = true
			haveField = true
		default:
			b.WriteRune(r)
			haveField = true
		}
	}
	if escaped {
		return nil, "dangling escape"
	}
	if inSingle {
		return nil, "unterminated single quote"
	}
	if inDouble {
		return nil, "unterminated double quote"
	}
	flush()
	return fields, ""
}
