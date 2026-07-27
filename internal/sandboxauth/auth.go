// Package sandboxauth owns authorization for model-requested sandbox capability
// deltas. It deliberately sits above sandbox materialization: sandbox evaluates
// and applies capability values, while this package decides whether the sealed
// delta may be selected for one concrete command.
package sandboxauth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/sandbox"
	"reasonix/internal/shellparse"
)

// ApprovalKind is the event kind used by capability approval prompts.
const ApprovalKind = "sandbox_capability"

// Action is an authoritative response to a capability prompt.
type Action string

const (
	// AllowOnce authorizes the complete capability bundle for this invocation only.
	AllowOnce Action = "allow_once"
	// AllowSession authorizes and records one reusable grant for this session.
	AllowSession Action = "allow_session"
	// AllowPersistent authorizes and requests persistence of one reusable grant.
	AllowPersistent Action = "allow_persistent"
	// RunSandboxed declines the delta and runs with the unchanged base sandbox.
	RunSandboxed Action = "run_sandboxed"
	// CancelCommand cancels the entire prepared command.
	CancelCommand Action = "cancel_command"
)

// Valid reports whether a response is one of the capability decision enum.
func (a Action) Valid() bool {
	switch a {
	case AllowOnce, AllowSession, AllowPersistent, RunSandboxed, CancelCommand:
		return true
	default:
		return false
	}
}

// Request is the complete host identity for one prepared invocation.
type Request struct {
	Review                      sandbox.CapabilityReview
	Workspace                   string
	Command                     string
	Background                  bool
	PreserveBackgroundProcesses bool
	Subagent                    bool
	Delegation                  Delegation
}

// Delegation is the parent-authorized filesystem ceiling for a sub-agent.
// Empty read/write roots grant no corresponding capability expansion.
type Delegation struct {
	ReadRoots  []string
	WriteRoots []string
}

// Decision selects the unchanged base sandbox or the complete sealed delta.
// CanonicalExecutable/Argv are an execution witness for reusable grants; a
// caller that supports direct execution must use them together.
type Decision struct {
	Use                 sandbox.CapabilityUse
	Cancel              bool
	Diagnostic          string
	Origin              DecisionOrigin
	Warnings            []Warning
	ConfigDiagnostics   []Diagnostic
	CanonicalExecutable string
	Argv                []string
}

// DecisionOrigin identifies the authority or fallback that selected a runtime decision.
type DecisionOrigin string

const (
	// OriginBaseSandbox means the requested delta was not applied.
	OriginBaseSandbox DecisionOrigin = "base_sandbox"
	// OriginYOLOAutoOnce means YOLO authorized this invocation without reusable state.
	OriginYOLOAutoOnce DecisionOrigin = "yolo_auto_once"
	// OriginSessionGrant means an in-memory same-session grant matched.
	OriginSessionGrant DecisionOrigin = "session_grant"
	// OriginProjectGrant means a persisted project grant matched.
	OriginProjectGrant DecisionOrigin = "project_grant"
	// OriginUserGrant means a persisted user-level grant matched.
	OriginUserGrant DecisionOrigin = "user_grant"
	// OriginHumanOnce means a fresh human allowed only this invocation.
	OriginHumanOnce DecisionOrigin = "human_once"
	// OriginHumanSession means a fresh human created or fell back to a session grant.
	OriginHumanSession DecisionOrigin = "human_session"
	// OriginHumanPersistent means a fresh human grant was persisted successfully.
	OriginHumanPersistent DecisionOrigin = "human_persistent"
)

// Warning is transport-neutral security information that frontends must not discard.
type Warning struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Mandatory bool   `json:"mandatory"`
}

// Prompt is the authoritative structured payload shown to the user.
type Prompt struct {
	Review                      sandbox.CapabilityReview `json:"review"`
	Workspace                   string                   `json:"workspace"`
	CanonicalExecutable         string                   `json:"canonical_executable"`
	Argv                        []string                 `json:"argv,omitempty"`
	GrantPrefix                 []string                 `json:"grant_prefix,omitempty"`
	Background                  bool                     `json:"background"`
	PreserveBackgroundProcesses bool                     `json:"preserve_background_processes"`
	Reusable                    bool                     `json:"reusable"`
	SuspectedSecret             bool                     `json:"suspected_secret"`
	Warnings                    []string                 `json:"warnings,omitempty"`
}

// Approver presents a strict-fresh capability decision. Implementations own
// prompt IDs and must keep invalid actions pending in their resolver.
type Approver interface {
	ApproveSandboxCapability(context.Context, Prompt) (Action, error)
}

// PolicyHook may only reduce authority. It is called for every requested
// capability, including a grant hit; true means continue authorization.
type PolicyHook interface {
	AllowSandboxCapability(context.Context, Request) (bool, string)
}

// GrantSource supplies already-validated project/runtime grants. Concrete
// configuration loading belongs to the caller (Issue #6), not this package.
type GrantSource interface {
	SandboxCapabilityGrants(context.Context, string) ([]Grant, []Diagnostic)
}

// Diagnostic identifies one disabled configuration entry without disabling siblings.
type Diagnostic struct {
	Source  string `json:"source"`
	Entry   int    `json:"entry"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AutoOncePolicy decides only the YOLO capability-specific branch. Defer leaves
// ordinary non-YOLO authorization unchanged; RunSandboxed prevents a prompt.
type AutoOncePolicy interface {
	DecideSandboxCapabilityAutoOnce(context.Context, Request) AutoOnceDecision
}

// AutoOnceAction is the exhaustive capability auto-once policy result.
type AutoOnceAction string

const (
	// AutoOnceDefer leaves authorization to grants and the ordinary approval flow.
	AutoOnceDefer AutoOnceAction = "defer"
	// AutoOnceAllow authorizes only the current invocation.
	AutoOnceAllow AutoOnceAction = "allow_once"
	// AutoOnceRunSandboxed executes with the unchanged base sandbox without prompting.
	AutoOnceRunSandboxed AutoOnceAction = "run_sandboxed"
)

// AutoOnceDecision carries the policy result and typed warning metadata.
type AutoOnceDecision struct {
	Action     AutoOnceAction
	Diagnostic string
	Warnings   []Warning
}

// AuditSink observes the final runtime decision without affecting authority.
type AuditSink interface {
	RecordSandboxCapabilityDecision(context.Context, AuditRecord)
}

// AuditRecord is the request/decision pair produced by one gate call.
type AuditRecord struct {
	Request  Request
	Decision Decision
	Err      string
	Visible  bool
}

// Persister is the project-grant boundary. Implementations are responsible for
// strict fresh reads, serialized comment-preserving mutation and atomic writes.
type Persister interface {
	SaveSandboxCapabilityGrant(context.Context, Grant) error
}

// Gate is the single execution-pipeline entry point.
type Gate interface {
	Authorize(context.Context, Request) (Decision, error)
}

// Grant is one atomic reusable capability authorization. Capabilities are never
// split or unioned across grants.
type Grant struct {
	Workspace                   string                `json:"workspace" toml:"workspace"`
	CanonicalExecutable         string                `json:"canonical_executable" toml:"canonical_executable"`
	ArgvPrefix                  []string              `json:"argv_prefix" toml:"argv_prefix"`
	Capabilities                sandbox.CapabilitySet `json:"capabilities" toml:"capabilities"`
	Background                  bool                  `json:"background" toml:"background"`
	PreserveBackgroundProcesses bool                  `json:"preserve_background_processes" toml:"preserve_background_processes"`
}

// Engine implements Gate with session, user-level, and optional project grants.
type Engine struct {
	Approver   Approver
	Hook       PolicyHook
	Source     GrantSource // project-level grants
	UserSource GrantSource // user-level grants (~/.reasonix/config.toml)
	AutoOnce   AutoOncePolicy
	Persister  Persister
	Audit      AuditSink

	mu      sync.RWMutex
	session []Grant
}

// yoloPolicyLifecycle is an optional cohesive extension. Custom AutoOncePolicy
// adapters can implement only the minimal authorization method.
type yoloPolicyLifecycle interface {
	AutoOncePolicy
	SetRuntimeMode(bool, bool)
	State() YOLOPolicyState
	Acknowledge(bool) bool
	SessionState() YOLOSessionState
	RestoreSessionState(YOLOSessionState)
	ClearSessionState()
	Configure(YOLOPolicyConfig)
	TakeStartupWarnings() []Warning
}

func (e *Engine) yoloLifecycle() (yoloPolicyLifecycle, bool) {
	policy, ok := e.AutoOnce.(yoloPolicyLifecycle)
	return policy, ok
}

// SetYOLORuntimeMode updates a policy that supports the built-in typed state.
func (e *Engine) SetYOLORuntimeMode(yolo, interactive bool) {
	if policy, ok := e.yoloLifecycle(); ok {
		policy.SetRuntimeMode(yolo, interactive)
	}
}

// YOLOState returns the typed auto-once policy state when configured.
func (e *Engine) YOLOState() (YOLOPolicyState, bool) {
	if policy, ok := e.yoloLifecycle(); ok {
		return policy.State(), true
	}
	return YOLOPolicyState{}, false
}

// AcknowledgeYOLOProjectExpansion accepts or refuses the pending expansion.
func (e *Engine) AcknowledgeYOLOProjectExpansion(accept bool) bool {
	if policy, ok := e.yoloLifecycle(); ok {
		return policy.Acknowledge(accept)
	}
	return false
}

// YOLOSessionState snapshots ephemeral same-session policy state for rebuilds.
func (e *Engine) YOLOSessionState() YOLOSessionState {
	if policy, ok := e.yoloLifecycle(); ok {
		return policy.SessionState()
	}
	return YOLOSessionState{}
}

// RestoreYOLOSessionState restores ephemeral state only when workspace matches.
func (e *Engine) RestoreYOLOSessionState(state YOLOSessionState) {
	if policy, ok := e.yoloLifecycle(); ok {
		policy.RestoreSessionState(state)
	}
}

// ConfigureYOLOPolicy applies reloaded configuration to the built-in policy.
func (e *Engine) ConfigureYOLOPolicy(cfg YOLOPolicyConfig) bool {
	if policy, ok := e.yoloLifecycle(); ok {
		policy.Configure(cfg)
		return true
	}
	return false
}

// TakeYOLOStartupWarnings returns newly pending mandatory headless warnings once.
func (e *Engine) TakeYOLOStartupWarnings() []Warning {
	if policy, ok := e.yoloLifecycle(); ok {
		return policy.TakeStartupWarnings()
	}
	return nil
}

// SessionGrants returns a defensive snapshot for same-session Controller rebuilds.
func (e *Engine) SessionGrants() []Grant {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return cloneGrants(e.session)
}

// RestoreSessionGrants carries grants across a Controller rebuild. Callers must
// not restore this state from transcript/history or across a workspace change.
func (e *Engine) RestoreSessionGrants(grants []Grant) {
	e.mu.Lock()
	e.session = cloneGrants(grants)
	e.mu.Unlock()
}

// ClearSessionGrants expires conversation-scoped authority on new/clear/resume.
func (e *Engine) ClearSessionGrants() {
	e.mu.Lock()
	e.session = nil
	e.mu.Unlock()
	if policy, ok := e.yoloLifecycle(); ok {
		policy.ClearSessionState()
	}
}

// Authorize applies hook, grant and strict-fresh approval decisions in order.
func (e *Engine) Authorize(ctx context.Context, req Request) (decision Decision, err error) {
	// Own every authority-bearing slice before crossing any observer or policy
	// boundary. Interface implementations are not trusted to treat values whose
	// structs contain slices as immutable.
	req = cloneRequest(req)
	if e.Audit != nil {
		defer func() {
			record := AuditRecord{Request: cloneRequest(req), Decision: cloneDecision(decision)}
			record.Visible = decision.Origin == OriginYOLOAutoOnce
			if err != nil {
				record.Err = err.Error()
			}
			e.Audit.RecordSandboxCapabilityDecision(ctx, record)
		}()
	}
	base := Decision{Use: sandbox.BaseOnly, Origin: OriginBaseSandbox}
	if !req.Review.Authority.Requested {
		return base, nil
	}
	if e.Hook != nil {
		allow, reason := e.Hook.AllowSandboxCapability(ctx, cloneRequest(req))
		if !allow {
			base.Diagnostic = firstNonEmpty(reason, "sandbox capability policy denied the request; using the base sandbox")
			return base, nil
		}
	}
	if req.Review.State != sandbox.CapabilityReady {
		base.Diagnostic = firstNonEmpty(req.Review.Diagnostic, "sandbox capability request is unavailable; using the base sandbox")
		return base, nil
	}
	if req.Subagent && !withinDelegation(req.Review.EffectiveDelta, req.Delegation) {
		base.Diagnostic = "sub-agent capability request exceeds delegated read/write authority; using the base sandbox"
		return base, nil
	}

	commandID, identityErr := commandIdentity(req)
	project, projectDiagnostics := e.projectGrants(ctx, req.Workspace)
	base.ConfigDiagnostics = cloneDiagnostics(projectDiagnostics)
	projectMessages := diagnosticMessages(projectDiagnostics)
	if identityErr == nil {
		session := e.SessionGrants()
		user, _ := e.userGrants(ctx, req.Workspace)
		// User grants are global across workspaces. Their Workspace remains the
		// user-home root used to canonicalize workspace-relative capability paths;
		// only session and project grants use it as an authorization scope.
		sources := []struct {
			grants          []Grant
			origin          DecisionOrigin
			workspaceScoped bool
		}{
			{grants: session, origin: OriginSessionGrant, workspaceScoped: true},
			{grants: user, origin: OriginUserGrant, workspaceScoped: false},
			{grants: project, origin: OriginProjectGrant, workspaceScoped: true},
		}
		for _, source := range sources {
			if grant, ok := matchingGrant(source.grants, commandID, req, source.workspaceScoped); ok {
				directArgv := append([]string(nil), commandID.argv...)
				directArgv[0] = grant.CanonicalExecutable
				return Decision{Use: sandbox.AuthorizedDelta, Origin: source.origin, Diagnostic: strings.Join(projectMessages, "; "), ConfigDiagnostics: cloneDiagnostics(projectDiagnostics), CanonicalExecutable: grant.CanonicalExecutable, Argv: directArgv}, nil
			}
		}
	}
	if e.AutoOnce != nil {
		auto := e.AutoOnce.DecideSandboxCapabilityAutoOnce(ctx, cloneRequest(req))
		diagnostic := strings.Join(append(projectMessages, auto.Diagnostic), "; ")
		switch auto.Action {
		case AutoOnceAllow:
			return Decision{Use: sandbox.AuthorizedDelta, Origin: OriginYOLOAutoOnce, Diagnostic: diagnostic, Warnings: append([]Warning(nil), auto.Warnings...), ConfigDiagnostics: cloneDiagnostics(projectDiagnostics)}, nil
		case AutoOnceRunSandboxed:
			base.Diagnostic = firstNonEmpty(diagnostic, "YOLO capability auto-approval is disabled; using the base sandbox")
			base.Warnings = append([]Warning(nil), auto.Warnings...)
			return base, nil
		}
	}
	if req.Subagent {
		base.Diagnostic = "sub-agent capability request has no eligible delegated grant; using the base sandbox"
		return base, nil
	}
	if e.Approver == nil {
		base.Diagnostic = "sandbox capability approval is unavailable; using the base sandbox"
		return base, nil
	}

	prompt := buildPrompt(req, commandID, identityErr)
	prompt.Warnings = append(prompt.Warnings, projectMessages...)
	action, err := e.Approver.ApproveSandboxCapability(ctx, clonePrompt(prompt))
	if err != nil {
		base.Diagnostic = fmt.Sprintf("sandbox capability approval failed: %v; using the base sandbox", err)
		return base, nil
	}
	switch action {
	case AllowOnce:
		return Decision{Use: sandbox.AuthorizedDelta, Origin: OriginHumanOnce, ConfigDiagnostics: cloneDiagnostics(projectDiagnostics)}, nil
	case AllowSession:
		if identityErr != nil {
			base.Diagnostic = "session capability grant is unavailable for this command; using the base sandbox"
			return base, nil
		}
		e.addSession(commandID.grant(req))
		return Decision{Use: sandbox.AuthorizedDelta, Origin: OriginHumanSession, ConfigDiagnostics: cloneDiagnostics(projectDiagnostics)}, nil
	case AllowPersistent:
		if identityErr != nil || prompt.SuspectedSecret {
			base.Diagnostic = "persistent capability grant is unsafe for this command; using the base sandbox"
			return base, nil
		}
		grant := commandID.grant(req)
		if e.Persister == nil {
			e.addSession(grant)
			return Decision{Use: sandbox.AuthorizedDelta, Origin: OriginHumanSession, Diagnostic: "project grant persistence is unavailable; downgraded to a session grant", ConfigDiagnostics: cloneDiagnostics(projectDiagnostics)}, nil
		}
		if err := e.Persister.SaveSandboxCapabilityGrant(ctx, cloneGrant(grant)); err != nil {
			e.addSession(grant)
			return Decision{Use: sandbox.AuthorizedDelta, Origin: OriginHumanSession, Diagnostic: fmt.Sprintf("persist capability grant: %v; downgraded to a session grant", err), ConfigDiagnostics: cloneDiagnostics(projectDiagnostics)}, nil
		}
		return Decision{Use: sandbox.AuthorizedDelta, Origin: OriginHumanPersistent, ConfigDiagnostics: cloneDiagnostics(projectDiagnostics)}, nil
	case CancelCommand:
		return Decision{Use: sandbox.BaseOnly, Cancel: true, Origin: OriginBaseSandbox, Diagnostic: "sandbox capability request canceled the command", ConfigDiagnostics: cloneDiagnostics(projectDiagnostics)}, nil
	case RunSandboxed:
		return base, nil
	default:
		base.Diagnostic = fmt.Sprintf("invalid sandbox capability action %q; using the base sandbox", action)
		return base, nil
	}
}

type identity struct {
	workspace  string
	executable string
	argv       []string
	prefix     []string
}

func commandIdentity(req Request) (identity, error) {
	var out identity
	workspace, err := filepath.Abs(req.Workspace)
	if err != nil {
		return out, fmt.Errorf("canonical workspace: %w", err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return out, fmt.Errorf("canonical workspace: %w", err)
	}
	cmd, err := shellparse.ParseReusableCommand(req.Command)
	if err != nil || len(cmd.Argv) == 0 {
		return out, errors.New("reusable grants require one static simple command")
	}
	if reusableCommandWrapper(cmd.Argv[0]) {
		return out, fmt.Errorf("reusable grants do not support command wrapper %q", cmd.Argv[0])
	}
	executable, err := exec.LookPath(cmd.Argv[0])
	if err != nil {
		return out, fmt.Errorf("resolve executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return out, fmt.Errorf("canonical executable: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(executable); rerr == nil {
		executable = resolved
	}
	prefix := append([]string(nil), req.Review.ArgvPrefix...)
	if len(prefix) == 0 || !argvHasPrefix(cmd.Argv, prefix) {
		prefix = append([]string(nil), cmd.Argv...)
	}
	return identity{workspace: workspace, executable: executable, argv: cmd.Argv, prefix: prefix}, nil
}

func buildPrompt(req Request, id identity, identityErr error) Prompt {
	p := Prompt{Review: req.Review, Workspace: id.workspace, CanonicalExecutable: id.executable, Argv: append([]string(nil), id.argv...), GrantPrefix: append([]string(nil), id.prefix...), Background: req.Background, PreserveBackgroundProcesses: req.PreserveBackgroundProcesses, Reusable: identityErr == nil}
	p.SuspectedSecret = suspectedSecret(id.argv)
	if identityErr != nil {
		// When command identity cannot be resolved (shell wrapper, pipeline,
		// etc.), populate executable/argv from the raw command string so the
		// frontend can still display what was requested.
		if id.executable == "" && req.Command != "" {
			parts := strings.Fields(req.Command)
			if len(parts) > 0 {
				p.CanonicalExecutable = parts[0]
				p.Argv = parts
			}
		}
		p.Warnings = append(p.Warnings, identityErr.Error())
	}
	if p.SuspectedSecret {
		p.Warnings = append(p.Warnings, "command arguments may contain a secret; persistent grants are disabled and session grants retain this command prefix until the session ends")
	}
	if req.PreserveBackgroundProcesses {
		p.Warnings = append(p.Warnings, "this reusable grant permits processes to survive command completion")
	}
	return p
}

func (id identity) grant(req Request) Grant {
	return Grant{Workspace: id.workspace, CanonicalExecutable: id.executable, ArgvPrefix: append([]string(nil), id.prefix...), Capabilities: cloneCapabilitySet(req.Review.EffectiveDelta), Background: req.Background, PreserveBackgroundProcesses: req.PreserveBackgroundProcesses}
}

func matchingGrant(grants []Grant, id identity, req Request, workspaceScoped bool) (Grant, bool) {
	for _, g := range grants {
		if (!workspaceScoped || g.Workspace == id.workspace) && g.CanonicalExecutable == id.executable && argvHasPrefix(id.argv, g.ArgvPrefix) && capabilitySetCovers(g.Capabilities, req.Review.EffectiveDelta) && (!req.Background || g.Background) && (!req.PreserveBackgroundProcesses || g.PreserveBackgroundProcesses) {
			return g, true
		}
	}
	return Grant{}, false
}

func capabilitySetCovers(granted, requested sandbox.CapabilitySet) bool {
	if requested.Network && !granted.Network {
		return false
	}
	if !capabilityPathsCover(granted.Reads, requested.Reads) || !capabilityPathsCover(granted.Writes, requested.Writes) {
		return false
	}
	for _, requestedDevice := range requested.Devices {
		covered := false
		for _, grantedDevice := range granted.Devices {
			if grantedDevice.Canonical == requestedDevice.Canonical && grantedDevice.Kind == requestedDevice.Kind &&
				grantedDevice.Major == requestedDevice.Major && grantedDevice.Minor == requestedDevice.Minor {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func capabilityPathsCover(granted, requested []sandbox.CapabilityPath) bool {
	for _, requestedPath := range requested {
		covered := false
		for _, grantedPath := range granted {
			if capabilityPathCovers(grantedPath, requestedPath) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func capabilityPathCovers(granted, requested sandbox.CapabilityPath) bool {
	if granted.Canonical == requested.Canonical {
		return granted.Kind == sandbox.CapabilityDirectory || granted.Kind == requested.Kind
	}
	return granted.Kind == sandbox.CapabilityDirectory && pathCoveredByRoots(requested.Canonical, []string{granted.Canonical})
}

func reusableCommandWrapper(name string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(name)), ".exe")
	switch base {
	case "sh", "bash", "dash", "zsh", "fish", "ksh", "csh", "tcsh",
		"pwsh", "powershell", "cmd", "command", "exec", "env", "sudo", "doas",
		"nohup", "timeout", "nice", "chroot", "setsid", "xargs", "script":
		return true
	default:
		return false
	}
}

func (e *Engine) projectGrants(ctx context.Context, workspace string) ([]Grant, []Diagnostic) {
	if e.Source == nil {
		return nil, nil
	}
	grants, diagnostics := e.Source.SandboxCapabilityGrants(ctx, workspace)
	return cloneGrants(grants), cloneDiagnostics(diagnostics)
}

// userGrants loads persisted user-level grants from the user config file.
func (e *Engine) userGrants(ctx context.Context, workspace string) ([]Grant, []Diagnostic) {
	if e.UserSource == nil {
		return nil, nil
	}
	grants, diagnostics := e.UserSource.SandboxCapabilityGrants(ctx, workspace)
	return cloneGrants(grants), cloneDiagnostics(diagnostics)
}

func withinDelegation(set sandbox.CapabilitySet, delegation Delegation) bool {
	for _, path := range set.Reads {
		if !pathCoveredByRoots(path.Canonical, delegation.ReadRoots) {
			return false
		}
	}
	for _, path := range set.Writes {
		if !pathCoveredByRoots(path.Canonical, delegation.WriteRoots) {
			return false
		}
	}
	return true
}

func pathCoveredByRoots(path string, roots []string) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (e *Engine) addSession(g Grant) {
	g = cloneGrant(g)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, existing := range e.session {
		if reflect.DeepEqual(existing, g) {
			return
		}
	}
	e.session = append(e.session, g)
}

func argvHasPrefix(argv, prefix []string) bool {
	if len(prefix) == 0 || len(prefix) > len(argv) {
		return false
	}
	for i := range prefix {
		if argv[i] != prefix[i] {
			return false
		}
	}
	return true
}

func suspectedSecret(argv []string) bool {
	for _, arg := range argv {
		lower := strings.ToLower(arg)
		for _, marker := range []string{"token=", "password=", "passwd=", "secret=", "api_key=", "apikey=", "authorization:"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

func cloneGrants(in []Grant) []Grant {
	out := append([]Grant(nil), in...)
	for i := range out {
		out[i] = cloneGrant(out[i])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CanonicalExecutable < out[j].CanonicalExecutable })
	return out
}

func cloneGrant(in Grant) Grant {
	in.ArgvPrefix = append([]string(nil), in.ArgvPrefix...)
	in.Capabilities = cloneCapabilitySet(in.Capabilities)
	return in
}

func cloneCapabilitySet(in sandbox.CapabilitySet) sandbox.CapabilitySet {
	in.Reads = append([]sandbox.CapabilityPath(nil), in.Reads...)
	in.Writes = append([]sandbox.CapabilityPath(nil), in.Writes...)
	in.Devices = append([]sandbox.CapabilityDevice(nil), in.Devices...)
	return in
}

func cloneReview(in sandbox.CapabilityReview) sandbox.CapabilityReview {
	in.Request = cloneCapabilitySet(in.Request)
	in.EffectiveDelta = cloneCapabilitySet(in.EffectiveDelta)
	in.ArgvPrefix = append([]string(nil), in.ArgvPrefix...)
	in.Risk.Findings = append([]sandbox.CapabilityRiskFinding(nil), in.Risk.Findings...)
	return in
}

func cloneRequest(in Request) Request {
	in.Review = cloneReview(in.Review)
	in.Delegation.ReadRoots = append([]string(nil), in.Delegation.ReadRoots...)
	in.Delegation.WriteRoots = append([]string(nil), in.Delegation.WriteRoots...)
	return in
}

func cloneDecision(in Decision) Decision {
	in.Argv = append([]string(nil), in.Argv...)
	in.Warnings = append([]Warning(nil), in.Warnings...)
	in.ConfigDiagnostics = cloneDiagnostics(in.ConfigDiagnostics)
	return in
}

func cloneDiagnostics(in []Diagnostic) []Diagnostic {
	return append([]Diagnostic(nil), in...)
}

func diagnosticMessages(in []Diagnostic) []string {
	out := make([]string, 0, len(in))
	for _, diagnostic := range in {
		out = append(out, diagnostic.Message)
	}
	return out
}

func clonePrompt(in Prompt) Prompt {
	in.Review = cloneReview(in.Review)
	in.Argv = append([]string(nil), in.Argv...)
	in.GrantPrefix = append([]string(nil), in.GrantPrefix...)
	in.Warnings = append([]string(nil), in.Warnings...)
	return in
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
