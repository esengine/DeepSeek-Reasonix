// Package permission decides, per tool call, whether to allow it, deny it, or
// ask the user first. The core is a pure Policy (rule evaluation, no I/O); a
// Gate wraps a Policy with an optional interactive Approver and is what the
// agent consults at execute time. Keeping rule evaluation pure makes it
// trivially testable and keeps the agent independent of how "ask" is resolved.
package permission

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/shellparse"
)

// Decision is the outcome of evaluating a tool call against a Policy.
type Decision int

const (
	// Allow runs the tool without prompting.
	Allow Decision = iota
	// Ask defers to an interactive Approver (or, with none, resolves to Allow).
	Ask
	// Deny blocks the tool in every mode.
	Deny
)

func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Ask:
		return "ask"
	case Deny:
		return "deny"
	default:
		return "unknown"
	}
}

// ParseDecision maps a config string to a Decision. Unknown / empty input
// defaults to Ask — the conservative posture for a writer fallback.
func ParseDecision(s string) Decision {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "allow":
		return Allow
	case "deny":
		return Deny
	default:
		return Ask
	}
}

// Rule matches tool calls. Tool is the tool name; Subject, when non-empty,
// constrains the call's subject. A glob Subject (see matchGlob) matches by
// wildcard; a Literal Subject matches by exact string equality. An empty Subject
// matches every call to Tool.
type Rule struct {
	Tool    string
	Subject string
	// Literal matches Subject by exact equality rather than as a glob, so a
	// remembered concrete command keeps any '*'/'?' as ordinary characters
	// instead of turning them into wildcards.
	Literal bool
}

// ParseRule parses "ToolName", "ToolName(glob)", or the legacy
// "ToolName=literal" form. Surrounding whitespace is trimmed. The "=literal"
// form (taken when the '=' precedes any '(') matches the rest of the string
// verbatim — no globbing — and is kept for existing configs that were written
// before the Claude Code-style Tool(specifier) approval rules. ok is false for
// a malformed entry (empty tool name) so the caller can warn rather than
// silently install a rule that matches nothing.
func ParseRule(s string) (Rule, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Rule{}, false
	}
	if eq := strings.IndexByte(s, '='); eq > 0 {
		if paren := strings.IndexByte(s, '('); paren < 0 || eq < paren {
			tool := strings.TrimSpace(s[:eq])
			if tool == "" {
				return Rule{}, false
			}
			return Rule{Tool: tool, Subject: s[eq+1:], Literal: true}, true
		}
	}
	if i := strings.IndexByte(s, '('); i >= 0 && strings.HasSuffix(s, ")") {
		tool := strings.TrimSpace(s[:i])
		if tool == "" {
			return Rule{}, false
		}
		return Rule{Tool: tool, Subject: s[i+1 : len(s)-1]}, true
	}
	return Rule{Tool: s}, true
}

func parseRules(ss []string) []Rule {
	var out []Rule
	for _, s := range ss {
		if r, ok := ParseRule(s); ok {
			out = append(out, r)
		}
	}
	return out
}

// Policy is a set of rules plus the writer fallback mode. It is the pure,
// I/O-free heart of the permission layer.
type Policy struct {
	// Mode is the fallback decision for writer tools when no rule matches.
	// Read-only tools always fall back to Allow.
	Mode  Decision
	Allow []Rule
	Ask   []Rule
	Deny  []Rule
	// SessionAllow is an explicit frontend/session override such as Claude
	// Code's --allowed-tools. Deny rules still win, while these rules override
	// configured Ask entries for the current process only.
	SessionAllow []Rule
}

// WithSessionAllow returns a copy of p with additional ephemeral allow rules.
// Malformed entries are ignored consistently with New.
func (p Policy) WithSessionAllow(rules []string) Policy {
	p.SessionAllow = append(append([]Rule(nil), p.SessionAllow...), parseRules(rules)...)
	return p
}

// New builds a Policy from config string slices and a mode string ("ask" by
// default). Malformed rule strings are dropped.
func New(mode string, allow, ask, deny []string) Policy {
	return Policy{
		Mode:  ParseDecision(mode),
		Allow: parseRules(allow),
		Ask:   parseRules(ask),
		Deny:  parseRules(deny),
	}
}

// Decide evaluates a tool call. readOnly is the tool's own classification; args
// is the raw JSON the model sent, from which the call's subject is extracted
// for glob matching. Calls with multiple subjects, such as move_file's source
// and destination paths, must be safe for every subject before the call is
// allowed. Precedence: deny > ask > allow > fallback (Allow for readers, Mode
// for writers). SessionAllow sits between deny and configured ask rules.
func (p Policy) Decide(toolName string, readOnly bool, args json.RawMessage) Decision {
	return p.DecideSubjects(toolName, readOnly, Subjects(args))
}

// ExplicitlyDenies reports only configured deny-rule matches. It deliberately
// excludes the fallback Mode so higher-priority local MCP policy can be applied
// before the global posture.
func (p Policy) ExplicitlyDenies(toolName string, args json.RawMessage) bool {
	subjects := Subjects(args)
	if len(subjects) == 0 {
		subjects = []string{""}
	}
	for _, subject := range subjects {
		if matchAny(p.Deny, toolName, subject) {
			return true
		}
	}
	return false
}

// DecideSubject evaluates a tool call when the caller already extracted the
// stable approval subject from args.
func (p Policy) DecideSubject(toolName string, readOnly bool, subject string) Decision {
	if canonicalRuleTool(toolName) == "bash" {
		switch {
		case matchAny(p.Deny, toolName, subject):
			return Deny
		case matchAny(p.SessionAllow, toolName, subject):
			return Allow
		case matchAny(p.Ask, toolName, subject):
			return Ask
		case matchAny(p.Allow, toolName, subject):
			return Allow
		}
		if parts := DecomposeBashCommand(subject); parts != nil {
			return p.decideBashSegments(readOnly, parts)
		}
	}
	switch {
	case matchAny(p.Deny, toolName, subject):
		return Deny
	case matchAny(p.SessionAllow, toolName, subject):
		return Allow
	case matchAny(p.Ask, toolName, subject):
		return Ask
	case matchAny(p.Allow, toolName, subject):
		return Allow
	case readOnly:
		return Allow
	default:
		return p.Mode
	}
}

// decideBashSegments evaluates each simple-command segment of a compound bash
// invocation against the rule table independently. This lets prefix rules like
// `Bash(git push:*)` — created by the existing auto-save path for atomic
// commands — cover common compound flows (`git add . && git commit && git
// push`) without ever synthesizing a new prefix from a compound command.
//
// Precedence stays deny > ask > allow > fallback. Any single segment hitting
// deny denies the whole call; any segment needing approval turns the whole
// call into Ask; the whole call is Allow only if every segment is covered or
// writer fallback allows uncovered segments.
// A segment recognized as read-only by shellsafe (echo/ls/git status/...) is
// allowed on its own without a rule, matching the behavior of an atomic
// read-only bash call.
func (p Policy) decideBashSegments(readOnly bool, parts []string) Decision {
	out := Allow
	for _, sub := range parts {
		segReadOnly := readOnly
		if !segReadOnly {
			if isReadOnlyBashSubject(sub) {
				segReadOnly = true
			}
		}
		switch {
		case matchAny(p.Deny, "bash", sub):
			return Deny
		case matchAny(p.SessionAllow, "bash", sub):
			// covered by the explicit session allowlist
		case matchAny(p.Ask, "bash", sub):
			out = Ask
		case matchAny(p.Allow, "bash", sub):
			// covered
		case segReadOnly:
			// covered
		default:
			// Segment not covered by a rule or read-only classification: apply
			// the same writer fallback used for atomic bash commands.
			switch p.Mode {
			case Deny:
				return Deny
			case Ask:
				out = Ask
			default:
				// covered by fallback allow
			}
		}
	}
	return out
}

// DecideSubjects evaluates a tool call against every subject the call touches.
// This keeps two-path operations honest: a move is denied if either endpoint is
// denied, asks if either endpoint requires approval, and is allowed only when
// every endpoint is allowed under the same policy.
func (p Policy) DecideSubjects(toolName string, readOnly bool, subjects []string) Decision {
	if len(subjects) == 0 {
		return p.DecideSubject(toolName, readOnly, "")
	}
	out := Allow
	for _, subject := range subjects {
		switch p.DecideSubject(toolName, readOnly, subject) {
		case Deny:
			return Deny
		case Ask:
			out = Ask
		}
	}
	return out
}

// matchAny reports whether any rule matches the (toolName, subject) pair. A
// subject-specific rule cannot match a call that exposes no subject.
func matchAny(rules []Rule, toolName, subject string) bool {
	for _, r := range rules {
		if !ruleToolMatches(r.Tool, toolName) {
			continue
		}
		if r.Subject == "" {
			return true
		}
		if subject == "" {
			continue
		}
		if ruleSubjectMatches(r, subject) {
			return true
		}
	}
	return false
}

// RuleMatchesString reports whether one config-style rule string matches the
// given tool subject. It is used for session grants as well as persisted config
// rules so both paths share identical matching semantics.
func RuleMatchesString(rule, toolName, subject string) bool {
	r, ok := ParseRule(rule)
	return ok && matchAny([]Rule{r}, toolName, subject)
}

// RuleCoversString reports whether every call represented by candidate is
// already covered by existing. It intentionally proves only the cases Reasonix
// creates automatically: exact rules covered by broader globs or bare tool
// rules, exact duplicate globs, and bare tool rules covering subject rules.
func RuleCoversString(existing, candidate string) bool {
	a, ok := ParseRule(existing)
	if !ok {
		return false
	}
	b, ok := ParseRule(candidate)
	if !ok {
		return false
	}
	if !ruleToolCompatible(a.Tool, b.Tool) {
		return false
	}
	if a.Subject == "" {
		return true
	}
	if b.Subject == "" {
		return false
	}
	if bashRulePrefixBaseMatches(a, b) {
		return true
	}
	if b.Literal || !hasGlobMeta(b.Subject) {
		return ruleSubjectMatches(a, b.Subject)
	}
	return !a.Literal && a.Subject == b.Subject
}

func hasGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?")
}

func bashRulePrefixBaseMatches(existing, candidate Rule) bool {
	if canonicalRuleTool(existing.Tool) != "bash" || canonicalRuleTool(candidate.Tool) != "bash" {
		return false
	}
	existingBase, ok := bashPrefixBase(existing.Subject)
	if !ok {
		return false
	}
	candidateBase, ok := bashPrefixBase(candidate.Subject)
	return ok && existingBase == candidateBase
}

// subjectKeys are the JSON argument keys, in priority order, that carry a tool
// call's "subject" — the thing a Subject glob matches against. Generic so tools
// need not implement a permission-specific method: bash exposes command, the
// file tools expose path / file_path, grep & glob expose pattern.
var subjectKeys = []string{"command", "file_path", "path", "source_path", "destination_path", "pattern"}

// Subject extracts the primary matchable subject string from a call's raw JSON
// args, returning "" when none of the known keys is present (such a call only
// matches bare "ToolName" rules). Use Subjects for permission decisions that
// must account for every touched endpoint.
func Subject(args json.RawMessage) string {
	subjects := Subjects(args)
	if len(subjects) > 0 {
		return subjects[0]
	}
	return ""
}

// Subjects extracts every matchable subject from a call's raw JSON args. Most
// tools expose one subject; move_file exposes both source_path and
// destination_path so path-scoped permission rules can protect either endpoint.
func Subjects(args json.RawMessage) []string {
	if len(args) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil
	}
	src := stringArg(m, "source_path")
	dst := stringArg(m, "destination_path")
	if src != "" && dst != "" {
		out := []string{src}
		if dst != src {
			out = append(out, dst)
		}
		return out
	}
	for _, k := range subjectKeys {
		if s := stringArg(m, k); s != "" {
			return []string{s}
		}
	}
	return nil
}

func stringArg(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// matchGlob reports whether name matches pattern, where '*' matches any run of
// characters (including separators) and '?' matches exactly one. Unlike
// path.Match, '*' is not stopped by '/', which is what command-line and path
// prefixes ("rm -rf*", "/etc/*") intuitively expect. Linear time with
// backtracking, byte-oriented.
func matchGlob(pattern, name string) bool {
	var px, nx, starPx, starNx int
	starPx = -1
	for nx < len(name) {
		switch {
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == name[nx]):
			px++
			nx++
		case px < len(pattern) && pattern[px] == '*':
			starPx = px
			starNx = nx
			px++
		case starPx != -1:
			px = starPx + 1
			starNx++
			nx = starNx
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

// Approver resolves an Ask decision interactively. Implementations live in the
// front-end (the chat TUI); a non-interactive run passes a nil Approver, which
// the Gate treats as "allow" to preserve autonomous behaviour.
type Approver interface {
	// Approve asks the user about a pending call. It returns whether to allow
	// it and whether to remember that choice as a new rule. A non-nil err (e.g.
	// the context was cancelled while waiting) aborts the turn.
	Approve(ctx context.Context, toolName, subject string, args json.RawMessage) (allow, remember bool, err error)
}

// ReasonedApprover is the optional extension used by frontends that can return
// a denial reason to feed back to the model.
type ReasonedApprover interface {
	ApproveWithReason(ctx context.Context, toolName, subject string, args json.RawMessage) (allow, remember bool, reason string, err error)
}

// FreshApprover handles safety decisions that must come from a current human
// and cannot be satisfied by an allow rule, Auto/YOLO, or a remembered choice.
type FreshApprover interface {
	ApproveFresh(ctx context.Context, toolName, subject string, args json.RawMessage) (allow bool, reason string, err error)
}

// MCPApprover resolves an MCP approval through the configured reviewer. It is
// separate from Approver because prompt/writes modes outrank the controller's
// global Auto/YOLO posture, and destructive calls must never reuse a remembered
// grant. reviewer is "user", "auto_review", or empty for legacy routing.
type MCPApprover interface {
	ApproveMCP(ctx context.Context, toolName, subject string, args json.RawMessage, destructive, forced bool, reviewer string) (allow bool, reason string, err error)
}

// Gate is what the agent consults at execute time: a Policy plus an optional
// Approver. It satisfies the agent's Gate interface structurally.
type Gate struct {
	Policy   Policy
	Approver Approver

	// OnRemember, when set, is invoked with a new allow rule the user chose to
	// remember (e.g. "Bash(go build)"), so the front-end can persist it.
	OnRemember func(rule string)
}

// NewGate wires a Policy to an Approver (nil for non-interactive use).
func NewGate(p Policy, a Approver) *Gate { return &Gate{Policy: p, Approver: a} }

// Check decides whether a tool call may run. It is the method the agent's Gate
// interface expects. A denied or refused call returns allow=false with a short
// reason the agent feeds back to the model.
func (g *Gate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	if toolName == "bash" && !readOnly {
		if BashCommandIsReadOnly(args) {
			readOnly = true
		}
	}
	switch g.Policy.Decide(toolName, readOnly, args) {
	case Deny:
		return false, "denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain.", nil
	case Ask:
		if g.Approver == nil {
			return true, "", nil // non-interactive: preserve autonomy
		}
		subject := Subject(args)
		allow, remember, approverReason, err := g.approve(ctx, toolName, subject, args)
		if err != nil {
			return false, "approval aborted", err
		}
		if !allow {
			reason := "the user declined this tool call — do not retry it; ask how they would like to proceed or choose another approach."
			if approverReason != "" {
				reason = approverReason
			}
			return false, reason, nil
		}
		if remember && g.OnRemember != nil {
			// "Always allow" is tool-wide: persist the bare tool name so any
			// later subject (a different file / command) is allowed without
			// re-prompting. Deny rules still take precedence on every call.
			g.OnRemember(toolName)
			// Also add the rule to the in-memory Policy immediately so it
			// takes effect in the current session without requiring a restart.
			// The session-level grant (controller.granted) already covers the
			// Approver path, but any code path that consults Policy.Decide()
			// directly would miss the rule until the next controller build.
			if rule, ok := ParseRule(toolName); ok {
				g.Policy.Allow = append(g.Policy.Allow, rule)
			}
		}
		return true, "", nil
	default:
		return true, "", nil
	}
}

// CheckFresh preserves explicit deny precedence, then requires a fresh approver
// regardless of ordinary allow/ask/fallback posture. It deliberately never
// persists or installs an in-memory allow rule.
func (g *Gate) CheckFresh(ctx context.Context, toolName, subject string, args json.RawMessage, readOnly bool) (bool, string, error) {
	if g.Policy.Decide(toolName, readOnly, args) == Deny {
		return false, "denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain.", nil
	}
	approver, ok := g.Approver.(FreshApprover)
	if !ok {
		return false, "this tool requires fresh human approval and cannot run in a non-interactive session.", nil
	}
	allow, reason, err := approver.ApproveFresh(ctx, toolName, subject, args)
	if err != nil {
		return false, "approval aborted", err
	}
	if !allow {
		if strings.TrimSpace(reason) == "" {
			reason = "the user declined this tool call"
		}
		return false, reason, nil
	}
	return true, "", nil
}

// CheckMCP applies MCP-local approval policy. Precedence is explicit deny,
// destructive fresh review, per-tool/server mode, then the ordinary global
// permission posture. Local prompt/writes decisions require an MCPApprover and
// therefore fail closed in headless and sub-agent sessions.
func (g *Gate) CheckMCP(ctx context.Context, toolName, subject string, args json.RawMessage, readOnly, destructive bool, mode, reviewer string) (bool, string, error) {
	if g.Policy.ExplicitlyDenies(toolName, args) {
		return false, "denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain.", nil
	}
	if destructive {
		return g.approveMCP(ctx, toolName, subject, args, true, true, reviewer)
	}

	switch normalizeMCPApprovalMode(mode) {
	case "approve":
		return true, "", nil
	case "prompt":
		return g.approveMCP(ctx, toolName, subject, args, false, true, reviewer)
	case "writes":
		if readOnly {
			return true, "", nil
		}
		return g.approveMCP(ctx, toolName, subject, args, false, true, reviewer)
	default: // auto preserves the existing global policy and posture.
		switch g.Policy.Decide(toolName, readOnly, args) {
		case Deny:
			return g.Check(ctx, toolName, args, readOnly)
		case Ask:
			if strings.TrimSpace(reviewer) != "" {
				return g.approveMCP(ctx, toolName, subject, args, false, false, reviewer)
			}
			return g.Check(ctx, toolName, args, readOnly)
		default:
			return true, "", nil
		}
	}
}

func (g *Gate) approveMCP(ctx context.Context, toolName, subject string, args json.RawMessage, destructive, forced bool, reviewer string) (bool, string, error) {
	approver, ok := g.Approver.(MCPApprover)
	if !ok {
		return false, "this MCP tool requires an approval reviewer and cannot run in a non-interactive session.", nil
	}
	allow, reason, err := approver.ApproveMCP(ctx, toolName, subject, args, destructive, forced, reviewer)
	if err != nil {
		if strings.TrimSpace(reason) == "" {
			reason = "approval review aborted"
		}
		return false, reason, err
	}
	if !allow {
		if strings.TrimSpace(reason) == "" {
			reason = "the MCP approval reviewer declined this tool call"
		}
		return false, reason, nil
	}
	return true, "", nil
}

func normalizeMCPApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		return "auto"
	case "approve", "prompt", "writes":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "prompt"
	}
}

func (g *Gate) approve(ctx context.Context, toolName, subject string, args json.RawMessage) (bool, bool, string, error) {
	if a, ok := g.Approver.(ReasonedApprover); ok {
		return a.ApproveWithReason(ctx, toolName, subject, args)
	}
	allow, remember, err := g.Approver.Approve(ctx, toolName, subject, args)
	return allow, remember, "", err
}

// rememberRule builds the rule string persisted when the user picks "always
// allow". Bash commands prefer a safe command prefix (e.g. go test:*) so
// "always allow" covers similar invocations with different arguments. File
// mutation tools are remembered tool-wide ("Edit") so approving one file edit
// covers all files. Other tools are remembered by tool name. Deny and ask rules keep their higher precedence.
func rememberRule(toolName, subject string) string {
	return RememberRuleForScope(toolName, subject)
}

// RememberRuleForScope builds the rule string persisted when the user chooses
// an always-allow option. Bash commands prefer a safe prefix (go test:*) so
// similar invocations (different search terms, different test packages) match;
// when no safe prefix can be extracted the exact command is used. File
// mutation tools are always remembered tool-wide (Edit). Other tools use their
// bare tool name. Deny rules still take precedence on every call.
func RememberRuleForScope(toolName, subject string) string {
	subject = strings.TrimSpace(subject)
	if subject != "" && toolName == "bash" {
		if pattern := BashCommandPrefix(subject); pattern != "" {
			return "Bash(" + pattern + ")"
		}
		return "Bash(" + subject + ")"
	}
	if IsFileMutationTool(toolName) {
		return "Edit"
	}
	return toolName
}

// SessionGrantKey returns the in-memory rule for "allow this session". Bash
// prefers a command prefix when one is available, falling back to the exact
// command when unsafe. File mutation tools share a single Edit grant.
func SessionGrantKey(toolName, subject string) string {
	return SessionGrantRuleForScope(toolName, subject)
}

// SessionGrantRuleForScope returns the in-memory rule for a session grant.
// Bash prefers a command prefix when one is available; file mutation tools
// share a single Edit grant; all other tools return the bare tool name.
func SessionGrantRuleForScope(toolName, subject string) string {
	subject = strings.TrimSpace(subject)
	if toolName == "bash" && subject != "" {
		if pattern := BashCommandPrefix(subject); pattern != "" {
			return "Bash(" + pattern + ")"
		}
		return "Bash(" + subject + ")"
	}
	if IsFileMutationTool(toolName) {
		return "Edit"
	}
	return toolName
}

// BashCommandPrefix returns a conservative prefix rule for "similar command"
// approvals. It avoids shell syntax and keeps the prefix at command-word
// boundaries, so approving "go test ./..." grants "go test:*" rather than a
// broader "go *".
func BashCommandPrefix(subject string) string {
	cmd := strings.TrimSpace(subject)
	if cmd == "" || containsShellSyntax(cmd) {
		return ""
	}
	if BashDangerWarning(cmd) != "" {
		return ""
	}
	fields, malformed := shellparse.StaticFields(cmd)
	if malformed != "" {
		return ""
	}
	if len(fields) < 2 {
		return ""
	}
	base := strings.ToLower(fields[0])
	if isPackageManagerRun(base) && len(fields) >= 3 && strings.ToLower(fields[1]) == "run" {
		return fields[0] + " " + fields[1] + " " + fields[2] + ":*"
	}
	return fields[0] + " " + fields[1] + ":*"
}

func isPackageManagerRun(base string) bool {
	switch base {
	case "npm", "pnpm", "yarn", "bun":
		return true
	default:
		return false
	}
}

// IsFileMutationTool reports whether a built-in tool mutates workspace files.
func IsFileMutationTool(toolName string) bool {
	switch toolName {
	case "write_file", "edit_file", "multi_edit", "move_file", "notebook_edit", "delete_range", "delete_symbol":
		return true
	default:
		return false
	}
}

func ruleToolMatches(ruleTool, toolName string) bool {
	ruleTool = canonicalRuleTool(ruleTool)
	return ruleTool == toolName || (ruleTool == "file_mutation" && IsFileMutationTool(toolName))
}

func ruleToolCompatible(existingTool, candidateTool string) bool {
	existingTool = canonicalRuleTool(existingTool)
	candidateTool = canonicalRuleTool(candidateTool)
	return existingTool == candidateTool ||
		(existingTool == "file_mutation" && (candidateTool == "file_mutation" || IsFileMutationTool(candidateTool)))
}

func canonicalRuleTool(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "Bash", "bash":
		return "bash"
	case "Edit", "edit", "file_mutation":
		return "file_mutation"
	default:
		return toolName
	}
}

func ruleSubjectMatches(rule Rule, subject string) bool {
	if rule.Subject == "" {
		return true
	}
	if subject == "" {
		return false
	}
	if rule.Literal {
		return rule.Subject == subject
	}
	if canonicalRuleTool(rule.Tool) == "bash" {
		if base, ok := bashColonPrefixBase(rule.Subject); ok {
			return bashPrefixMatches(base, subject)
		}
		if base, ok := legacyBashSpaceStarPrefixBase(rule.Subject); ok {
			return bashPrefixMatches(base, subject)
		}
	}
	return matchGlob(rule.Subject, subject)
}

func bashColonPrefixBase(pattern string) (string, bool) {
	if !strings.HasSuffix(pattern, ":*") {
		return "", false
	}
	base := strings.TrimSuffix(pattern, ":*")
	return base, base != ""
}

func legacyBashSpaceStarPrefixBase(pattern string) (string, bool) {
	if !strings.HasSuffix(pattern, " *") {
		return "", false
	}
	base := strings.TrimSuffix(pattern, " *")
	return base, base != ""
}

func bashPrefixBase(pattern string) (string, bool) {
	if base, ok := bashColonPrefixBase(pattern); ok {
		return base, true
	}
	return legacyBashSpaceStarPrefixBase(pattern)
}

func bashPrefixMatches(base, subject string) bool {
	if normalized, ok := normalizeBashSafeRedirectsForMatch(subject); ok {
		subject = normalized
	}
	fields, malformed := shellparse.StaticFields(subject)
	if malformed != "" {
		return false
	}
	baseFields, malformed := shellparse.StaticFields(base)
	if malformed != "" || len(baseFields) == 0 || len(fields) < len(baseFields) {
		return false
	}
	for i, want := range baseFields {
		if fields[i] != want {
			return false
		}
	}
	return true
}
