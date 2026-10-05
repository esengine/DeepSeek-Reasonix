package control

// The approval bridge: what answers the agent's permission gate, and the text
// each kind of ask shows a human. It is declared sensitive in REASONIX.md
// because these decide whether a tool call runs.

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/permission"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/memory"
)

// denyPermissionApprover answers for a session nobody is watching: a headless
// run has no prompt to show, so a call that needs approval can only be refused.
type denyPermissionApprover struct {
	// folder is set when the refusals exist because the workspace folder is not
	// trusted, the one cause the person can remove for good.
	folder *folderRefusal
}

// folderRefusal is why a headless run opened on the asking posture: the folder
// it works in holds no trust decision that lets edits and commands run.
type folderRefusal struct {
	root     string
	declined bool
	// trusted is the policy trusting the folder would run the session under.
	trusted permission.Policy
}

func (denyPermissionApprover) Approve(context.Context, string, string, json.RawMessage) (bool, bool, error) {
	return false, false, nil
}

// Unattended marks this refusal as nobody's: permission reports it apart from
// a person declining.
func (denyPermissionApprover) Unattended() bool { return true }

// RefusalCodeFor names the folder-trust cause only for a call trusting the
// folder would let through.
func (a denyPermissionApprover) RefusalCodeFor(tool, subject, policyReason string, args json.RawMessage) string {
	if a.folder.explains(tool, subject, policyReason, args) {
		return permission.RefusalUntrustedFolder
	}
	return ""
}

// UntrustedFolderCause is why a headless run in this folder asks, in one place
// for the model, the run summary and the result.
func UntrustedFolderCause(declined bool) string {
	if declined {
		return "is not trusted: the person declined trust for it earlier"
	}
	return "is not trusted: no trust decision is recorded for it"
}

// UntrustedFolderRemedy is what the person does so this folder's edits and
// commands stop needing approval. Plain `trust` shows what the folder's own
// files would run before approving; `--yes` skips that, so it is not offered.
func UntrustedFolderRemedy(root string, declined bool) string {
	dir := "<this folder>"
	if !strings.ContainsAny(root, "\r\n\x00") {
		dir = "'" + strings.ReplaceAll(root, "'", `'\''`) + "'"
	}
	remedy := fmt.Sprintf("review and trust it with `reasonix trust --dir %s` (add --yes only after reading what it lists), or pass --permission-mode auto for this run only", dir)
	if declined {
		return "if the person has changed their mind, " + remedy
	}
	return remedy
}

// explains reports whether the folder is what refused this call: no rule asks
// about it, no class of call needs a person whatever the posture, so a trusted
// folder would let it run.
func (f *folderRefusal) explains(tool, subject, policyReason string, args json.RawMessage) bool {
	return f != nil && policyReason == "" && explicitApprovalReason(tool, subject) == "" && !RequiresFreshHumanApprovalTool(tool) &&
		f.trusted.Decide(tool, false, args) == permission.Allow
}

func (f folderRefusal) reason() string {
	return fmt.Sprintf("this workspace folder %s %s, so file edits and shell commands in it need approval, and this run has nobody to give it. Nobody declined this call and retrying or rewriting it cannot change that. The person can %s. Do the part that needs no approval, then call conclude_blocked naming the untrusted folder.", strconv.Quote(f.root), UntrustedFolderCause(f.declined), UntrustedFolderRemedy(f.root, f.declined))
}

// ApproveWithReason says which refusal this is: without a reason the gate reports
// "the user declined this tool call", untrue when there was no user, and the
// model goes to ask someone who was never there. A call only a person may
// answer leads with why, so the model learns which of its steps needed one.
func (a denyPermissionApprover) ApproveWithReason(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, string, error) {
	return a.ApproveWithPolicyReason(ctx, tool, subject, args, "")
}

// ApproveWithPolicyReason is ApproveWithReason knowing which rule asked, so a
// refusal the folder's trust would not lift is not blamed on the folder.
func (a denyPermissionApprover) ApproveWithPolicyReason(_ context.Context, tool, subject string, args json.RawMessage, policyReason string) (bool, bool, string, error) {
	if a.folder.explains(tool, subject, policyReason, args) {
		return false, false, a.folder.reason(), nil
	}
	reason := "this session has no interactive approver, so any call that needs approval is refused — nobody declined it, and neither retrying nor rewriting it can change that. If this work is meant to run unattended, it needs a permission mode that does not ask (or an explicit allow rule for this tool). Otherwise do the part that needs no approval and call conclude_blocked naming what was refused."
	if why := explicitApprovalReason(tool, subject); why != "" {
		reason = why + " " + reason
	}
	return false, false, reason, nil
}

// rulesWithoutFreshHumanApproval drops any session-allow rule that targets a
// tool requiring fresh human approval, so an explicit allowlist cannot bypass
// the always-prompt contract for those tools.
func rulesWithoutFreshHumanApproval(rules []permission.Rule) []permission.Rule {
	if len(rules) == 0 {
		return rules
	}
	filtered := make([]permission.Rule, 0, len(rules))
	for _, r := range rules {
		if RequiresFreshHumanApprovalTool(r.Tool) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// approval bridge (agent gate → events)

// gateApprover adapts the Controller to permission.Approver. It is distinct
// from the public Approve command (different signature, different direction).
type gateApprover struct{ c *Controller }

// The classes of call no posture answers for. The code is the identity a
// window renders in the reader's language; the sentence is what the model is
// told, and it stays in the language the model is addressed in.
const (
	dynamicBashApproval      = "dynamic_bash"
	browserCredentialApprova = "browser_credential"
	browserScriptApproval    = "browser_script"
	computerUseApproval      = "computer_use"
	computerPointerApproval  = "computer_pointer"
	computerFrontApproval    = "computer_front"
)

// computerActTakesFront is whether operating an application can take the
// foreground: Windows delivers keys only to the window in front.
var computerActTakesFront = runtime.GOOS == "windows"

var explicitApprovalTexts = map[string]string{
	dynamicBashApproval:      "This command uses nested or indirect shell execution. Auto and broad allow rules cannot verify the inner command; approve this exact command or use YOLO.",
	browserCredentialApprova: "This browser step types a password, a one-time code or card details into the site. Auto, the site's grant and broad allow rules do not answer it; approve it or use YOLO.",
	browserScriptApproval:    "This browser step runs arbitrary JavaScript with the page's authority. Auto, the site's grant and broad allow rules do not answer it; approve it or use YOLO.",
	computerPointerApproval:  "This takes the pointer the person is holding — it moves their cursor, clicks with it, and brings the application forward — rather than asking an element to act. Auto, the application's own grant and broad allow rules do not answer it; approve it or use YOLO.",
	computerUseApproval:      "This reads or operates another application on the computer, with whatever access that application has. Auto and broad allow rules do not answer it; approve it for this application or use YOLO.",
	computerFrontApproval:    "This operates another application on the computer, with whatever access that application has. Windows sends keys only to the window in front, so typing, key presses and pastes bring that application forward first. Auto and broad allow rules do not answer it; approve it for this application or use YOLO.",
}

// ExplicitApprovalCode names why a call needs a person rather than auto or a
// broad rule, or "" when it does not. It is derived from the call, so whoever
// renders the prompt asks rather than being told.
func ExplicitApprovalCode(tool, subject string) string {
	switch {
	case strings.EqualFold(tool, "bash") && permission.BashSubjectRequiresExplicitApproval(subject):
		return dynamicBashApproval
	case permission.IsBrowserTool(tool) && strings.HasPrefix(subject, permission.BrowserScriptPrefix):
		return browserScriptApproval
	case permission.IsBrowserTool(tool) && permission.BrowserSubjectRequiresExplicitApproval(subject):
		return browserCredentialApprova
	case permission.IsComputerTool(tool) && permission.ComputerSubjectTakesPointer(subject):
		return computerPointerApproval
	case permission.IsComputerTool(tool) && tool == "computer_act" && computerActTakesFront:
		return computerFrontApproval
	case permission.IsComputerTool(tool):
		return computerUseApproval
	}
	return ""
}

// explicitApprovalReason is that same answer as the sentence the model reads.
func explicitApprovalReason(tool, subject string) string {
	return explicitApprovalTexts[ExplicitApprovalCode(tool, subject)]
}

func (g gateApprover) Approve(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	allow, remember, _, err := g.ApproveWithReason(ctx, tool, subject, args)
	return allow, remember, err
}

func (g gateApprover) ApproveWithReason(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, string, error) {
	return g.approveWithPolicyReason(ctx, tool, subject, args, "")
}

func (g gateApprover) ApproveWithPolicyReason(ctx context.Context, tool, subject string, args json.RawMessage, policyReason string) (bool, bool, string, error) {
	return g.approveWithPolicyReason(ctx, tool, subject, args, policyReason)
}

func combineApprovalReasons(reasons ...string) string {
	var kept []string
	for _, reason := range reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			kept = append(kept, reason)
		}
	}
	return strings.Join(kept, "\n")
}

func (g gateApprover) approveWithPolicyReason(ctx context.Context, tool, subject string, args json.RawMessage, policyReason string) (bool, bool, string, error) {
	if tool == memoryRememberTool && g.c.allowLowRiskRemember(args) {
		return true, false, "", nil
	}
	subject = approvalDisplaySubject(tool, subject, args)
	humanReason := explicitApprovalReason(tool, subject)
	requireHuman := humanReason != ""
	// Check pre-approval first, before any prompt or Guardian review. Dynamic
	// Bash accepts only YOLO or an exact session grant here; ordinary calls also
	// accept the just-approved-plan window. Deny rules already bit at the policy
	// level before this point.
	if requireHuman && g.c.approval.preApprovedForRequiredHuman(tool, subject) {
		return true, false, "", nil
	}
	if !requireHuman && g.c.approval.preApproved(tool, subject, args) {
		return true, false, "", nil
	}
	if g.c.guardianSess != nil && !requireHuman {
		allow, reason, reviewErr := g.c.guardianSess.Review(ctx, tool, args, g.c.executor.Session())
		if reviewErr != nil {
			return false, false, "", reviewErr
		}
		if allow && !requiresFreshApprovalTool(tool) {
			return true, false, "", nil
		}
		reason = combineApprovalReasons(policyReason, reason)
		humanAllow, remember, err := g.c.requestApproval(ctx, approvalRequest{tool: tool, subject: subject, args: args, reason: reason})
		if err != nil {
			return false, false, reason, err
		}
		if !humanAllow {
			return false, false, reason, nil
		}
		return true, remember, "", nil
	}
	if requireHuman {
		reason := combineApprovalReasons(policyReason, humanReason)
		allow, remember, err := g.c.requestApproval(ctx, approvalRequest{tool: tool, subject: subject, args: args, reason: reason, requireHuman: true})
		return allow, remember, "", err
	}
	allow, remember, err := g.c.requestApproval(ctx, approvalRequest{tool: tool, subject: subject, args: args, reason: policyReason})
	return allow, remember, "", err
}

type sandboxEscapeApprover struct{ c *Controller }

func (s sandboxEscapeApprover) ApproveSandboxEscape(ctx context.Context, req sandbox.EscapeRequest) (bool, string, error) {
	subject := sandboxEscapeApprovalSubject(req.Command)
	reason := sandboxEscapeApprovalReason(req.Reason)
	reply, err := s.c.requestApprovalDecision(ctx, approvalRequest{tool: SandboxEscapeApprovalTool, subject: subject, args: req.Args, reason: reason, fresh: true})
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.SandboxEscapeDeclined, nil
	}
	if reply.session {
		s.c.approval.grantSession(SandboxEscapeApprovalTool, subject)
	}
	return true, "", nil
}

func (s sandboxEscapeApprover) SandboxEscapeSessionAllowed(_ context.Context, req sandbox.EscapeRequest) bool {
	return s.c.approval.preApprovedForDecision(SandboxEscapeApprovalTool, sandboxEscapeApprovalSubject(req.Command), nil, true)
}

// ApproveEgress asks whether bash may reach a host the allow list does not
// name. The user configured that list, so no approval mode answers for them;
// only a person, once or for the rest of the session, widens it.
func (s sandboxEscapeApprover) ApproveEgress(ctx context.Context, host string) (bool, error) {
	reply, err := s.c.requestApprovalDecision(ctx, approvalRequest{tool: NetworkEgressApprovalTool, subject: host,
		reason: fmt.Sprintf(i18n.M.EgressApprovalReasonFmt, host), fresh: true})
	if err != nil || !reply.allow {
		return false, err
	}
	if reply.session {
		s.c.approval.grantSession(NetworkEgressApprovalTool, host)
	}
	return true, nil
}

func sandboxEscapeApprovalSubject(command string) string {
	subject := strings.TrimSpace(command)
	if subject == "" {
		return i18n.M.SandboxEscapeSubjectFallback
	}
	return i18n.M.SandboxEscapeSubjectPrefix + subject
}

func sandboxEscapeApprovalReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return i18n.M.SandboxEscapeRuntimeReason
	}
	return reason
}

// managedConfigWriteApprover routes a file tool's Reasonix-managed config write
// through the fresh-human approval prompt (see ManagedConfigWriteApprovalTool).
// A session grant is tool-wide (mirroring sandbox_escape): one "allow for this
// session" covers the rest of the repair flow across the handful of managed
// config files without re-prompting on every incremental edit.
type managedConfigWriteApprover struct{ c *Controller }

func (m managedConfigWriteApprover) ApproveManagedConfigWrite(ctx context.Context, req tool.ConfigWriteRequest) (bool, string, error) {
	subject := managedConfigWriteApprovalSubject(req.Path)
	args, _ := json.Marshal(map[string]string{"path": req.Path})
	reply, err := m.c.requestApprovalDecision(ctx, approvalRequest{tool: ManagedConfigWriteApprovalTool, subject: subject, args: args, reason: i18n.M.ConfigWriteReason, fresh: true})
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.ConfigWriteDeclined, nil
	}
	if reply.session {
		m.c.approval.grantSession(ManagedConfigWriteApprovalTool, subject)
	}
	return true, "", nil
}

func (m managedConfigWriteApprover) ManagedConfigWriteSessionAllowed(_ context.Context, req tool.ConfigWriteRequest) bool {
	return m.c.approval.preApprovedForDecision(ManagedConfigWriteApprovalTool, managedConfigWriteApprovalSubject(req.Path), nil, true)
}

func managedConfigWriteApprovalSubject(path string) string {
	return i18n.M.ConfigWriteSubjectPrefix + strings.TrimSpace(path)
}

func approvalDisplaySubject(tool, subject string, args json.RawMessage) string {
	switch tool {
	case memoryRememberTool:
		return rememberApprovalSubject(subject, args)
	case memoryForgetTool:
		return forgetApprovalSubject(subject, args)
	case "move_file":
		return moveApprovalSubject(subject, args)
	default:
		return subject
	}
}

func moveApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		SourcePath      string `json:"source_path"`
		DestinationPath string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	if in.SourcePath == "" || in.DestinationPath == "" {
		return fallback
	}
	return in.SourcePath + " -> " + in.DestinationPath
}

func rememberApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	name := approvalCompactText(firstNonEmpty(in.Name, in.Title))
	desc := approvalTruncate(approvalCompactText(in.Description), 180)
	body := approvalTruncate(approvalCompactText(in.Body), 240)
	typ := string(memory.NormalizeType(in.Type))

	var b strings.Builder
	b.WriteString(i18n.M.MemoryApprovalSaveUpdate)
	baseLen := b.Len()
	if name != "" {
		fmt.Fprintf(&b, " %q", name)
	}
	if typ != "" {
		fmt.Fprintf(&b, " [%s]", typ)
	}
	if desc != "" {
		b.WriteString(": ")
		b.WriteString(desc)
	}
	if body != "" {
		if desc == "" {
			b.WriteString(": ")
		} else {
			b.WriteString(" | ")
		}
		b.WriteString(i18n.M.MemoryApprovalBodyLabel)
		b.WriteString(": ")
		b.WriteString(body)
	}
	if b.Len() == baseLen && fallback != "" {
		return fallback
	}
	return b.String()
}

func forgetApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	name := approvalCompactText(in.Name)
	if name == "" {
		return fallback
	}
	return fmt.Sprintf(i18n.M.MemoryApprovalArchiveFmt, name)
}

func approvalCompactText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func approvalTruncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// approvalRequest is one ask: what is being approved, why, and which postures
// may answer it instead of a human. The zero value is an ordinary tool
// permission with no stated reason.
type approvalRequest struct {
	tool    string
	subject string
	args    json.RawMessage
	reason  string
	// fresh marks a user trust/business decision rather than an ordinary tool
	// permission. It may reuse an explicit session grant, but YOLO/auto approval
	// must not answer or drain the prompt.
	fresh bool
	// requireHuman marks an ordinary tool approval that Auto, an approved-plan
	// window, Guardian, or an allowing hook must not answer. Unlike fresh it
	// retains the ordinary four-choice UI and YOLO remains an explicit bypass.
	requireHuman bool
}

// SharedHeadlessGate is the one gate every headless-only sub-agent surface
// (task, writer-capable skills, the planner) holds. They capture it once with
// no rebuild hook, so a posture switch reaches them only through Update here;
// otherwise they stay on whatever mode was active when they were built.
type SharedHeadlessGate struct {
	mu     sync.RWMutex
	policy permission.Policy
	gate   *freshHumanHeadlessGate
}

// NewSharedHeadlessGate builds a shared gate holder from the base policy and
// the initial approval mode (see BuildHeadlessApprovalGate for the mode
// contract).
func NewSharedHeadlessGate(policy permission.Policy, mode string) *SharedHeadlessGate {
	g := &SharedHeadlessGate{policy: policy}
	g.Update(mode)
	return g
}

// Update rebuilds the held gate for a new approval mode. Safe to call
// concurrently with Check (a turn may be mid-flight on another goroutine when
// the user switches modes).
func (g *SharedHeadlessGate) Update(mode string) {
	g.UpdateFor(mode, nil)
}

// UpdateFor is Update for a run whose asking posture comes from an untrusted
// folder, so the sub-agents' refusals name it too.
func (g *SharedHeadlessGate) UpdateFor(mode string, folder *folderRefusal) {
	g.set(buildHeadlessGate(g.policy, mode, folder))
}

// UpdateAttended is Update for a session a person is watching. A sub-agent
// cannot ask them, so a command the parent would put to them stays refused
// for the sub-agent too instead of Auto opening dynamic shell for it alone.
func (g *SharedHeadlessGate) UpdateAttended(mode string) {
	next := BuildHeadlessApprovalGate(g.policy, mode)
	if normalizeToolApprovalMode(mode) == ToolApprovalAuto {
		next.gate.Policy.AllowDynamicBash = g.policy.AllowDynamicBash
	}
	g.set(next)
}

func (g *SharedHeadlessGate) set(next *freshHumanHeadlessGate) {
	g.mu.Lock()
	g.gate = next
	g.mu.Unlock()
}

// RuleAllows reports whether a configured allow rule already covers this call.
// The fallback mode deliberately does not count: "auto approves writers" is a
// posture, while a matched rule is the user's own answer written down, and only
// the second one may stand in for asking again.
func (g *SharedHeadlessGate) RuleAllows(toolName string, args json.RawMessage, readOnly bool) bool {
	// Neutralizing the writer fallback is what separates the two: with Mode set
	// to Ask, an Allow can only have come from a rule that matched. Reading the
	// rule list directly would have to re-implement bash's segment matching.
	probe := g.policy
	probe.Mode = permission.Ask
	return probe.Decide(toolName, readOnly, args) == permission.Allow
}

func (g *SharedHeadlessGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	return g.current().Check(ctx, toolName, args, readOnly)
}

// Verdict is Check with the refusal's identity.
func (g *SharedHeadlessGate) Verdict(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (permission.Verdict, error) {
	return g.current().Verdict(ctx, toolName, args, readOnly)
}

// DeniesWriters reports whether the current posture is read-only.
func (g *SharedHeadlessGate) DeniesWriters() bool { return g.current().DeniesWriters() }

func (g *SharedHeadlessGate) current() *freshHumanHeadlessGate {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.gate
}

func (g *SharedHeadlessGate) ExplicitlyDenies(toolName string, args json.RawMessage) bool {
	g.mu.RLock()
	gate := g.gate
	g.mu.RUnlock()
	return gate.ExplicitlyDenies(toolName, args)
}

type freshHumanHeadlessGate struct {
	gate                    *permission.Gate
	dynamicBashBypass       bool
	allowLowRiskFreshAction func(toolName string, args json.RawMessage) bool
}

func (g *freshHumanHeadlessGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	v, err := g.Verdict(ctx, toolName, args, readOnly)
	return v.Allow, v.Reason, err
}

// Verdict is Check with the refusal's identity. A read-only posture answers
// first, so no narrower refusal below it can stand in for the real cause.
func (g *freshHumanHeadlessGate) Verdict(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (permission.Verdict, error) {
	if g.gate.DeniesWriters() {
		if v, err := g.gate.Verdict(ctx, toolName, args, readOnly); err != nil || !v.Allow {
			return v, err
		}
	}
	if RequiresFreshHumanApprovalTool(toolName) {
		if !g.gate.ExplicitlyDenies(toolName, args) && !g.gate.DeniesWriters() &&
			g.allowLowRiskFreshAction != nil &&
			g.allowLowRiskFreshAction(toolName, args) {
			return permission.Verdict{Allow: true}, nil
		}
		return permission.Verdict{Reason: "this tool requires fresh human approval and cannot run in a non-interactive session. Use an interactive session or a user-initiated memory command.", Code: permission.RefusalUnattended}, nil
	}
	if strings.EqualFold(toolName, "bash") {
		if blocker := permission.BashSubjectApprovalBlocker(permission.Subject(args)); blocker != permission.BashApprovalBlockerNone {
			if g.gate.Policy.Decide(toolName, readOnly, args) != permission.Allow && !g.dynamicBashBypass {
				return permission.Verdict{Reason: headlessBashBlockReason(blocker), Code: permission.RefusalUnattended}, nil
			}
		}
	}
	return g.gate.Verdict(ctx, toolName, args, readOnly)
}

// DeniesWriters reports a read-only posture.
func (g *freshHumanHeadlessGate) DeniesWriters() bool { return g.gate.DeniesWriters() }

func (g *freshHumanHeadlessGate) ExplicitlyDenies(toolName string, args json.RawMessage) bool {
	return g.gate.Policy.ExplicitlyDenies(toolName, args)
}

// Every blocked shape leads callers toward writing a script, so the writable
// boundary belongs in all of them: told only to use a file, models reach for
// /tmp and lose a second round to the sandbox.
const headlessBashBlockSuffix = " Scratch files belong under $TMPDIR, the session-private directory the host provides; a literal /tmp path is not writable, and anything else must be inside the workspace. The user can also switch to an interactive session or YOLO mode."

// headlessBashBlockReason names the shape that actually stopped the command.
// A caller told "inline interpreter code is blocked" about `env | grep` learns
// nothing and rewrites toward the wrong fix.
func headlessBashBlockReason(blocker permission.BashApprovalBlocker) string {
	const lead = "this shell command requires human approval and cannot run in a non-interactive session. "
	switch blocker {
	case permission.BashApprovalBlockerInlineCode:
		return lead + "It carries inline interpreter code (python -c, node -e, bash -c), which the host cannot audit; write the code to a file with write_file and run that file instead (e.g. write repro.py, then `python3 repro.py`)." + headlessBashBlockSuffix
	case permission.BashApprovalBlockerNestedExecution:
		return lead + "It nests another command through substitution ($(...), `...`, or <(...)), so what would actually run is not visible in the command; split it into separate calls and pass the values literally." + headlessBashBlockSuffix
	case permission.BashApprovalBlockerDynamicName:
		return lead + "The program it runs comes from a variable, so the host cannot tell what would execute; name the program literally." + headlessBashBlockSuffix
	case permission.BashApprovalBlockerIndirectExecution:
		return lead + "It runs through a wrapper (eval, source, xargs, env without a command, find -exec) that executes something the command itself does not name; call the target program directly." + headlessBashBlockSuffix
	case permission.BashApprovalBlockerHereDocBody:
		return lead + "It feeds a here-document, whose body is file content rather than arguments the host can read; write the file with write_file, then run whatever needs it." + headlessBashBlockSuffix
	default:
		return lead + "The host could not statically read what it would run; use a simpler literal command, or read_file/grep for inspection." + headlessBashBlockSuffix
	}
}
