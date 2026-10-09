// Package permission decides, per tool call, whether to allow it, deny it, or
// ask the user first. The core is a pure Policy (rule evaluation, no I/O); a
// Gate wraps a Policy with an optional interactive Approver and is what the
// agent consults at execute time. Keeping rule evaluation pure makes it
// trivially testable and keeps the agent independent of how "ask" is resolved.
package permission

import (
	"context"
	"encoding/json"
	"reasonix/internal/contract/permrule"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/shellsafe"
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

// Rule is a parsed permission rule; its syntax lives in contract/permrule.
type Rule = permrule.Rule

// ParseRule parses one rule; see permrule.Parse.
func ParseRule(s string) (Rule, bool) { return permrule.Parse(s) }

func legacyBarePowerShellDenyCmdlet(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "set-content":
		return "Set-Content", true
	case "add-content":
		return "Add-Content", true
	case "out-file":
		return "Out-File", true
	default:
		return "", false
	}
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

func parseDenyRules(ss []string) []Rule {
	var out []Rule
	for _, s := range ss {
		r, ok := ParseRule(s)
		if !ok {
			continue
		}
		// Preserve the generic ToolName meaning while also recognizing the three
		// bare PowerShell write cmdlets accepted by older Desktop settings as
		// command prefixes. The compatibility expansion is deny-only and
		// additive, so it cannot broaden an allow or weaken an exact tool deny.
		out = append(out, r)
		if r.Subject == "" {
			if cmdlet, ok := legacyBarePowerShellDenyCmdlet(r.Tool); ok {
				out = append(out, Rule{Tool: "Bash", Subject: cmdlet + ":*"})
			}
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
	// AllowDynamicBash lets the writer fallback Mode cover command
	// substitution and interpreter -c/-e forms. It is deliberately opt-in:
	// broad Bash allow rules alone must not re-open nested-command bypasses.
	AllowDynamicBash bool
	ReadOnly         bool // denies every call that is not a read, whatever the rules allow
}

// WithSessionAllow returns a copy of p with additional ephemeral allow rules.
// Malformed entries are ignored consistently with New.
func (p Policy) WithSessionAllow(rules []string) Policy {
	p.SessionAllow = append(append([]Rule(nil), p.SessionAllow...), parseRules(rules)...)
	return p
}

// WithAllowDynamicBashFallback enables the explicit advanced override for
// dynamic shell shapes. Deny, ask, and exact allow rules retain precedence.
func (p Policy) WithAllowDynamicBashFallback(enabled bool) Policy {
	p.AllowDynamicBash = enabled
	return p
}

// New builds a Policy from config string slices and a mode string ("ask" by
// default). Malformed rule strings are dropped.
func New(mode string, allow, ask, deny []string) Policy {
	return Policy{
		Mode:  ParseDecision(mode),
		Allow: parseRules(allow),
		Ask:   parseRules(ask),
		Deny:  parseDenyRules(deny),
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
// excludes the fallback Mode so installing or explicitly authorizing an MCP
// server remains the final allow decision.
func (p Policy) ExplicitlyDenies(toolName string, args json.RawMessage) bool {
	subjects := Subjects(args)
	if len(subjects) == 0 {
		subjects = []string{""}
	}
	for _, subject := range subjects {
		if matchAnyRaw(p.Deny, toolName, subject) {
			return true
		}
	}
	return false
}

// DecideSubject evaluates a tool call when the caller already extracted the
// stable approval subject from args.
func (p Policy) DecideSubject(toolName string, readOnly bool, subject string) Decision {
	if p.ReadOnly && !readOnly {
		return Deny
	}
	if canonicalRuleTool(toolName) == "bash" {
		return p.decideBashSubject(toolName, readOnly, subject)
	}
	if d, ok := p.decideBrowserCredential(toolName, subject); ok {
		return d
	}
	if d, ok := p.decideComputer(toolName, subject); ok {
		return d
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
	case subjectRequiresHuman(toolName, subject):
		return Ask
	default:
		return p.Mode
	}
}

func (p Policy) decideBashSubject(toolName string, readOnly bool, subject string) Decision {
	approvalClass := classifyBashApproval(subject)
	requiresExact := approvalClass != bashApprovalReusable
	requiresHuman := approvalClass == bashApprovalRequireHuman
	parts := DecomposeBashCommand(subject)
	switch {
	case matchAnyRaw(p.Deny, toolName, subject):
		return Deny
	case matchAnyExact(p.SessionAllow, toolName, subject):
		return Allow
	case !requiresExact && parts == nil && matchAnyAllow(p.SessionAllow, toolName, subject):
		return Allow
	case matchAnyRaw(p.Ask, toolName, subject):
		return Ask
	case matchAnyExact(p.Allow, toolName, subject):
		return Allow
	}
	if parts != nil {
		return p.decideBashSegments(readOnly, parts)
	}
	// A remembered prefix rule never covers a network path: the user approved a
	// command, not a connection to whatever machine a later argument names.
	if shellsafe.OperandsNameNetworkPath(subject) {
		requiresHuman = true
	}
	switch {
	case requiresHuman && p.Mode == Deny:
		return Deny
	case requiresHuman && p.AllowDynamicBash && p.Mode == Allow:
		return Allow
	case requiresHuman:
		return Ask
	case requiresExact && readOnly:
		return Allow
	case requiresExact:
		return p.Mode
	}
	switch {
	case matchAnyAllow(p.Allow, toolName, subject):
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
			segReadOnly = isReadOnlyBashSubject(sub)
		}
		switch p.DecideSubject("bash", segReadOnly, sub) {
		case Deny:
			return Deny
		case Ask:
			out = Ask
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

func matchAnyRaw(rules []Rule, toolName, subject string) bool {
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
		if rawRuleSubjectMatches(r, subject) {
			return true
		}
	}
	return false
}

func firstMatchingRule(rules []Rule, toolName, subject string, raw bool) (Rule, bool) {
	for _, rule := range rules {
		if !ruleToolMatches(rule.Tool, toolName) {
			continue
		}
		if rule.Subject == "" {
			return rule, true
		}
		if subject == "" {
			continue
		}
		matches := ruleSubjectMatches(rule, subject)
		if raw {
			matches = rawRuleSubjectMatches(rule, subject)
		}
		if matches {
			return rule, true
		}
	}
	return Rule{}, false
}

func ruleConfigString(rule Rule) string {
	if rule.Subject == "" {
		return rule.Tool
	}
	if rule.Literal {
		return rule.Tool + "=" + rule.Subject
	}
	return rule.Tool + "(" + rule.Subject + ")"
}

// MatchedRule reports the configured rule responsible for an explicit Ask or
// Deny decision. Fallback-mode and dynamic-safety decisions intentionally have
// no rule provenance. Compound Bash commands are inspected segment by segment
// using the same raw-prefix semantics as DecideSubject.
func (p Policy) MatchedRule(toolName string, decision Decision, args json.RawMessage) (string, bool) {
	var rules []Rule
	switch decision {
	case Ask:
		rules = p.Ask
	case Deny:
		rules = p.Deny
	default:
		return "", false
	}
	subjects := Subjects(args)
	if len(subjects) == 0 {
		subjects = []string{""}
	}
	raw := canonicalRuleTool(toolName) == "bash"
	for _, subject := range subjects {
		candidates := []string{subject}
		if raw {
			if parts := DecomposeBashCommand(subject); parts != nil {
				candidates = append(candidates, parts...)
			}
		}
		for _, candidate := range candidates {
			// A matching configured rule is provenance only when that candidate's
			// actual decision has the same outcome. SessionAllow may override an
			// Ask rule on one endpoint while a different endpoint falls back to
			// Ask; reporting the overridden rule would misstate why the call was
			// stopped.
			if p.DecideSubject(toolName, false, candidate) != decision {
				continue
			}
			if rule, ok := firstMatchingRule(rules, toolName, candidate, raw); ok {
				return ruleConfigString(rule), true
			}
		}
	}
	return "", false
}

func rawRuleSubjectMatches(rule Rule, subject string) bool {
	if rule.Literal {
		return rule.Subject == subject
	}
	if canonicalRuleTool(rule.Tool) == "bash" {
		if base, ok := bashPrefixBase(rule.Subject); ok {
			return rawBashPrefixMatches(base, subject)
		}
	}
	return matchGlob(rule.Subject, subject)
}

// rawBashPrefixMatches reports whether a Bash rule's command prefix names the
// subject's program, or the program a wrapper in shellparse.PeelWrappers hands
// it to. Only deny and ask matching reach it; allow rules keep the command
// as written.
func rawBashPrefixMatches(base, subject string) bool {
	baseFields, malformed := shellparse.StaticFields(base)
	if malformed == "" && len(baseFields) > 0 {
		if features, ok := shellparse.AnalyzeApprovalFeatures(subject); ok && prefixFieldsMatch(baseFields, features.CommandPrefix) {
			return true
		}
		if inv, ok := shellparse.PeelWrappers(subject); ok {
			for _, layer := range inv.Layers {
				if prefixFieldsMatch(baseFields, layer) {
					return true
				}
			}
		}
	}
	base = strings.TrimSpace(base)
	subject = strings.TrimSpace(subject)
	if subject == base || (isCaseInsensitivePowerShellCmdlet(base) && strings.EqualFold(subject, base)) {
		return true
	}
	if len(subject) <= len(base) {
		return false
	}
	prefixMatches := strings.HasPrefix(subject, base)
	if isCaseInsensitivePowerShellCmdlet(base) {
		prefixMatches = strings.EqualFold(subject[:len(base)], base)
	}
	if !prefixMatches {
		return false
	}
	switch subject[len(base)] {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

func prefixFieldsMatch(baseFields, fields []string) bool {
	if len(fields) < len(baseFields) {
		return false
	}
	for i, want := range baseFields {
		got := fields[i]
		if got != want && !(i == 0 && isCaseInsensitivePowerShellCmdlet(want) && strings.EqualFold(got, want)) {
			return false
		}
	}
	return true
}

func isCaseInsensitivePowerShellCmdlet(s string) bool {
	_, ok := legacyBarePowerShellDenyCmdlet(s)
	return ok
}

func matchAnyExact(rules []Rule, toolName, subject string) bool {
	if subject == "" {
		return false
	}
	for _, r := range rules {
		if !ruleToolMatches(r.Tool, toolName) || r.Subject == "" {
			continue
		}
		if r.Subject == subject && (r.Literal || !hasGlobMeta(r.Subject)) {
			return true
		}
	}
	return false
}

func matchAnyAllow(rules []Rule, toolName, subject string) bool {
	if matchAnyExact(rules, toolName, subject) {
		return true
	}
	if canonicalRuleTool(toolName) == "bash" && bashSubjectRequiresExactRule(subject) ||
		IsBrowserTool(toolName) && BrowserSubjectRequiresExplicitApproval(subject) {
		return false
	}
	return matchAny(rules, toolName, subject)
}

// RuleMatchesString reports whether one config-style rule string matches the
// given tool subject. It is used for session grants as well as persisted config
// rules so both paths share identical matching semantics.
func RuleMatchesString(rule, toolName, subject string) bool {
	r, ok := ParseRule(rule)
	return ok && matchAnyAllow([]Rule{r}, toolName, subject)
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
	if b.Subject == "" {
		return a.Subject == ""
	}
	if canonicalRuleTool(b.Tool) == "bash" && (b.Literal || !hasGlobMeta(b.Subject)) && bashSubjectRequiresExactRule(b.Subject) {
		return matchAnyExact([]Rule{a}, canonicalRuleTool(b.Tool), b.Subject)
	}
	if a.Subject == "" {
		return true
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

// PolicyReasonedApprover receives the explicit permission-rule provenance that
// caused an Ask decision. Frontends can display it without duplicating Policy
// matching logic; older Approver implementations remain source-compatible.
type PolicyReasonedApprover interface {
	ApproveWithPolicyReason(ctx context.Context, toolName, subject string, args json.RawMessage, policyReason string) (allow, remember bool, reason string, err error)
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

// ExplicitlyDenies reports whether an explicit deny rule matches. Authorized
// MCP servers use this narrow view so install-time authorization is not
// followed by redundant per-call approval prompts.
func (g *Gate) ExplicitlyDenies(toolName string, args json.RawMessage) bool {
	return g.Policy.ExplicitlyDenies(toolName, args)
}

func (g *Gate) approve(ctx context.Context, toolName, subject string, args json.RawMessage, policyReason string) (bool, bool, string, error) {
	if a, ok := g.Approver.(PolicyReasonedApprover); ok {
		return a.ApproveWithPolicyReason(ctx, toolName, subject, args, policyReason)
	}
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
		return "Bash=" + subject
	}
	if rule, ok := groupGrantRule(toolName, subject); ok {
		return rule
	}
	return toolName
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
		return "Bash=" + subject
	}
	if rule, ok := groupGrantRule(toolName, subject); ok {
		return rule
	}
	return sessionGrantRule(toolName, subject)
}

// BashCommandPrefix returns a conservative prefix rule for "similar command"
// approvals. It avoids shell syntax and keeps the prefix at command-word
// boundaries, so approving "go test ./..." grants "go test:*" rather than a
// broader "go *".
func BashCommandPrefix(subject string) string {
	cmd := strings.TrimSpace(subject)
	if cmd == "" || containsShellSyntax(cmd) || bashSubjectRequiresExactRule(cmd) {
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

func ruleToolMatches(ruleTool, toolName string) bool {
	ruleTool = canonicalRuleTool(ruleTool)
	return ruleTool == toolName || inGroup(ruleTool, toolName)
}

func ruleToolCompatible(existingTool, candidateTool string) bool {
	existingTool = canonicalRuleTool(existingTool)
	candidateTool = canonicalRuleTool(candidateTool)
	return existingTool == candidateTool || inGroup(existingTool, candidateTool)
}

func canonicalRuleTool(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "Bash", "bash":
		return "bash"
	case "Edit", "edit", "file_mutation":
		return fileMutationGroup
	case "Browser", "browser":
		return browserGroup
	case "Computer", "computer":
		return computerGroup
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
