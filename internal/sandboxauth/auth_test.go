package sandboxauth

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
)

type actionApprover struct {
	action Action
	calls  int
	prompt Prompt
}

func (a *actionApprover) ApproveSandboxCapability(_ context.Context, p Prompt) (Action, error) {
	a.calls++
	a.prompt = p
	return a.action, nil
}

type denyHook struct{ calls int }

func (h *denyHook) AllowSandboxCapability(context.Context, Request) (bool, string) {
	h.calls++
	return false, "hook denied"
}

type rewritingHook struct{}

func (rewritingHook) AllowSandboxCapability(_ context.Context, req Request) (bool, string) {
	req.Review.EffectiveDelta.Network = true
	req.Review.EffectiveDelta.Reads = nil
	if len(req.ReusableArgv) != 0 {
		req.ReusableArgv[0] = "env"
	}
	return true, ""
}

type rewritingAudit struct{ replacement string }

func (a rewritingAudit) RecordSandboxCapabilityDecision(_ context.Context, record AuditRecord) {
	if len(record.Request.Review.EffectiveDelta.Reads) != 0 {
		record.Request.Review.EffectiveDelta.Reads[0].Canonical = a.replacement
	}
}

type memoryAudit struct{ records []AuditRecord }

func (a *memoryAudit) RecordSandboxCapabilityDecision(_ context.Context, record AuditRecord) {
	a.records = append(a.records, record)
}

func readyReview(network bool) sandbox.CapabilityReview {
	set := sandbox.CapabilitySet{Network: network}
	return sandbox.CapabilityReview{
		State: sandbox.CapabilityReady, EffectiveDelta: set,
		Authority:  sandbox.CapabilityAuthorityStatus{Requested: true, Supported: true},
		ArgvPrefix: []string{"sh"},
	}
}

func TestSessionGrantRequiresOneCompleteMatchingGrant(t *testing.T) {
	workspace := t.TempDir()
	approver := &actionApprover{action: AllowSession}
	engine := &Engine{Approver: approver}
	req := Request{Review: readyReview(true), Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	first, err := engine.Authorize(context.Background(), req)
	if err != nil || first.Use != sandbox.AuthorizedDelta || approver.calls != 1 {
		t.Fatalf("first authorize = %+v err=%v calls=%d", first, err, approver.calls)
	}
	second, err := engine.Authorize(context.Background(), req)
	if err != nil || second.Use != sandbox.AuthorizedDelta || approver.calls != 1 || second.CanonicalExecutable == "" {
		t.Fatalf("grant hit = %+v err=%v calls=%d", second, err, approver.calls)
	}

	// A different complete bundle cannot be manufactured by the existing grant.
	approver.action = RunSandboxed
	different := req
	different.Review = readyReview(false)
	different.Review.EffectiveDelta.Reads = []sandbox.CapabilityPath{{Canonical: workspace, Kind: sandbox.CapabilityDirectory}}
	got, err := engine.Authorize(context.Background(), different)
	if err != nil || got.Use != sandbox.BaseOnly || approver.calls != 2 {
		t.Fatalf("different bundle = %+v err=%v calls=%d", got, err, approver.calls)
	}
}

func TestMissingReusableArgvCannotReuseCapabilityGrant(t *testing.T) {
	workspace := t.TempDir()
	safe := Request{Review: readyReview(true), Workspace: workspace, Command: "printf safe", ReusableArgv: []string{"printf", "safe"}}
	id, err := commandIdentity(safe)
	if err != nil {
		t.Fatal(err)
	}
	grant := id.grant(safe)
	grant.ArgvPrefix = []string{"printf"}

	req := Request{Review: readyReview(true), Workspace: workspace, Command: `printf $(touch denied)`}
	missApprover := &actionApprover{action: RunSandboxed}
	matcher := &Engine{
		Approver: missApprover,
		Source:   memoryGrantSource{grants: []Grant{grant}},
	}
	got, err := matcher.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly || missApprover.calls != 1 {
		t.Fatalf("existing grant decision=%+v err=%v approver_calls=%d", got, err, missApprover.calls)
	}

	creator := &Engine{Approver: &actionApprover{action: AllowSession}}
	got, err = creator.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly || len(creator.SessionGrants()) != 0 {
		t.Fatalf("new grant decision=%+v err=%v grants=%+v", got, err, creator.SessionGrants())
	}
}

func TestOneGrantMayCoverANarrowerCompleteBundleButPartialGrantsNeverUnion(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "root")
	child := filepath.Join(root, "child")
	executable, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("printf unavailable")
	}
	executable, _ = filepath.Abs(executable)
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	request := readyReview(true)
	request.EffectiveDelta.Reads = []sandbox.CapabilityPath{{Canonical: child, Kind: sandbox.CapabilityFile}}
	request.EffectiveDelta.Devices = []sandbox.CapabilityDevice{{Canonical: "/dev/example", Kind: sandbox.CapabilityCharacterDevice, Major: 1, Minor: 2}}
	req := Request{Review: request, Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	canonicalWorkspace, _ := filepath.EvalSymlinks(workspace)
	complete := Grant{
		Workspace: canonicalWorkspace, CanonicalExecutable: executable, ArgvPrefix: []string{"printf"},
		Capabilities: sandbox.CapabilitySet{
			Network: true,
			Reads:   []sandbox.CapabilityPath{{Canonical: root, Kind: sandbox.CapabilityDirectory}},
			Devices: []sandbox.CapabilityDevice{{Canonical: "/dev/example", Kind: sandbox.CapabilityCharacterDevice, Major: 1, Minor: 2}},
		},
	}
	engine := &Engine{Source: memoryGrantSource{grants: []Grant{complete}}}
	got, err := engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.AuthorizedDelta {
		t.Fatalf("complete covering grant = %+v err=%v", got, err)
	}

	partials := []Grant{complete, complete}
	partials[0].Capabilities.Devices = nil
	partials[1].Capabilities.Network = false
	partials[1].Capabilities.Reads = nil
	engine = &Engine{Source: memoryGrantSource{grants: partials}}
	got, err = engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly {
		t.Fatalf("partial grants were unioned: %+v err=%v", got, err)
	}
}

func TestHookDenialRunsBeforeGrantReuse(t *testing.T) {
	workspace := t.TempDir()
	approver := &actionApprover{action: AllowSession}
	audit := &memoryAudit{}
	engine := &Engine{Approver: approver, Audit: audit}
	req := Request{Review: readyReview(true), Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	if _, err := engine.Authorize(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	hook := &denyHook{}
	engine.Hook = hook
	got, err := engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly || got.Diagnostic != "hook denied" || hook.calls != 1 {
		t.Fatalf("hook result = %+v err=%v calls=%d", got, err, hook.calls)
	}
	if len(audit.records) != 2 || audit.records[1].Decision.Use != sandbox.BaseOnly {
		t.Fatalf("audit records=%+v", audit.records)
	}
}

func TestAdaptersCannotRewriteAuthorityOrStoredSessionGrants(t *testing.T) {
	workspace := t.TempDir()
	original := filepath.Join(workspace, "original")
	replacement := filepath.Join(workspace, "replacement")
	executable, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("printf unavailable")
	}
	executable, _ = filepath.Abs(executable)
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	canonicalWorkspace, _ := filepath.EvalSymlinks(workspace)

	request := readyReview(false)
	request.EffectiveDelta.Reads = []sandbox.CapabilityPath{{Canonical: original, Kind: sandbox.CapabilityFile}}
	req := Request{Review: request, Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	engine := &Engine{
		Hook: rewritingHook{},
		Source: memoryGrantSource{grants: []Grant{{
			Workspace: canonicalWorkspace, CanonicalExecutable: executable, ArgvPrefix: []string{"printf"},
			Capabilities: sandbox.CapabilitySet{Network: true},
		}}},
	}
	got, err := engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly {
		t.Fatalf("hook manufactured authority: decision=%+v err=%v", got, err)
	}
	if req.ReusableArgv[0] != "printf" {
		t.Fatalf("hook rewrote caller-owned reusable argv: %v", req.ReusableArgv)
	}

	approver := &actionApprover{action: AllowSession}
	engine = &Engine{Approver: approver, Audit: rewritingAudit{replacement: replacement}}
	got, err = engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.AuthorizedDelta || len(engine.SessionGrants()) != 1 {
		t.Fatalf("create session grant: decision=%+v err=%v", got, err)
	}
	grants := engine.SessionGrants()
	if grants[0].Capabilities.Reads[0].Canonical != original {
		t.Fatalf("audit rewrote stored grant: %+v", grants[0])
	}
	grants[0].Capabilities.Reads[0].Canonical = replacement
	if engine.SessionGrants()[0].Capabilities.Reads[0].Canonical != original {
		t.Fatal("SessionGrants returned authority-bearing slice aliases")
	}

	approver.action = RunSandboxed
	replacementReq := req
	replacementReq.Review = cloneReview(req.Review)
	replacementReq.Review.EffectiveDelta.Reads[0].Canonical = replacement
	got, err = engine.Authorize(context.Background(), replacementReq)
	if err != nil || got.Use != sandbox.BaseOnly || approver.calls != 2 {
		t.Fatalf("mutated session grant authorized replacement: decision=%+v err=%v calls=%d", got, err, approver.calls)
	}
}

func TestSubagentAndUnsupportedRequestsFallBackWithoutPrompt(t *testing.T) {
	approver := &actionApprover{action: AllowOnce}
	engine := &Engine{Approver: approver}
	workspace := t.TempDir()
	req := Request{Review: readyReview(true), Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}, Subagent: true}
	got, err := engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly || approver.calls != 0 {
		t.Fatalf("subagent miss = %+v err=%v calls=%d", got, err, approver.calls)
	}
	req.Review.State = sandbox.CapabilitySoftDenied
	req.Review.Diagnostic = "invalid path"
	got, err = engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly || got.Diagnostic != "invalid path" || approver.calls != 0 {
		t.Fatalf("unsupported = %+v err=%v calls=%d", got, err, approver.calls)
	}
}

type failingPersister struct{}

func (failingPersister) SaveSandboxCapabilityGrant(context.Context, Grant) error {
	return errors.New("disk full")
}

type memoryPersister struct {
	grants []Grant
}

func (p *memoryPersister) SaveSandboxCapabilityGrant(_ context.Context, grant Grant) error {
	p.grants = append(p.grants, grant)
	return nil
}

type memoryGrantSource struct{ grants []Grant }

func (s memoryGrantSource) SandboxCapabilityGrants(context.Context, string) ([]Grant, []Diagnostic) {
	return cloneGrants(s.grants), nil
}

type allowAutoOnce struct{ calls int }

func (p *allowAutoOnce) DecideSandboxCapabilityAutoOnce(context.Context, Request) AutoOnceDecision {
	p.calls++
	return AutoOnceDecision{Action: AutoOnceAllow}
}

func TestSuppliedProjectGrantAndAutoOncePolicy(t *testing.T) {
	workspace := t.TempDir()
	req := Request{Review: readyReview(true), Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	id, err := commandIdentity(req)
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{Source: memoryGrantSource{grants: []Grant{id.grant(req)}}}
	got, err := engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.AuthorizedDelta || got.CanonicalExecutable == "" || got.Argv[0] != got.CanonicalExecutable {
		t.Fatalf("source grant = %+v err=%v", got, err)
	}

	auto := &allowAutoOnce{}
	engine = &Engine{AutoOnce: auto}
	req.Subagent = true
	got, err = engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.AuthorizedDelta || auto.calls != 1 || len(engine.SessionGrants()) != 0 {
		t.Fatalf("auto once = %+v err=%v calls=%d grants=%d", got, err, auto.calls, len(engine.SessionGrants()))
	}
}

func TestSubagentDelegationCeilingAppliesBeforeGrantAndAutoOnce(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	review := readyReview(false)
	review.EffectiveDelta.Reads = []sandbox.CapabilityPath{{Canonical: outside, Kind: sandbox.CapabilityDirectory}}
	req := Request{Review: review, Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}, Subagent: true, Delegation: Delegation{ReadRoots: []string{workspace}}}
	auto := &allowAutoOnce{}
	engine := &Engine{AutoOnce: auto}
	got, err := engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.BaseOnly || auto.calls != 0 {
		t.Fatalf("delegation result = %+v err=%v auto_calls=%d", got, err, auto.calls)
	}
}

func TestPersistentFailureDowngradesToSession(t *testing.T) {
	workspace := t.TempDir()
	approver := &actionApprover{action: AllowPersistent}
	engine := &Engine{Approver: approver, Persister: failingPersister{}}
	req := Request{Review: readyReview(true), Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	got, err := engine.Authorize(context.Background(), req)
	if err != nil || got.Use != sandbox.AuthorizedDelta || len(engine.SessionGrants()) != 1 {
		t.Fatalf("persistent fallback = %+v err=%v grants=%d", got, err, len(engine.SessionGrants()))
	}
}

func TestCapabilityApprovalActionsHaveDistinctRuntimeSemantics(t *testing.T) {
	for _, tc := range []struct {
		action      Action
		wantUse     sandbox.CapabilityUse
		wantCancel  bool
		wantSession int
		wantPersist int
	}{
		{action: AllowOnce, wantUse: sandbox.AuthorizedDelta},
		{action: AllowSession, wantUse: sandbox.AuthorizedDelta, wantSession: 1},
		{action: AllowPersistent, wantUse: sandbox.AuthorizedDelta, wantPersist: 1},
		{action: RunSandboxed, wantUse: sandbox.BaseOnly},
		{action: CancelCommand, wantUse: sandbox.BaseOnly, wantCancel: true},
	} {
		t.Run(string(tc.action), func(t *testing.T) {
			persister := &memoryPersister{}
			engine := &Engine{Approver: &actionApprover{action: tc.action}, Persister: persister}
			got, err := engine.Authorize(context.Background(), Request{
				Review: readyReview(true), Workspace: t.TempDir(), Command: "printf ok", ReusableArgv: []string{"printf", "ok"},
			})
			if err != nil || got.Use != tc.wantUse || got.Cancel != tc.wantCancel ||
				len(engine.SessionGrants()) != tc.wantSession || len(persister.grants) != tc.wantPersist {
				t.Fatalf("decision=%+v err=%v session=%d persisted=%d", got, err, len(engine.SessionGrants()), len(persister.grants))
			}
		})
	}
}

func TestSuspectedSecretsAllowOnceWarnForSessionAndDisablePersistence(t *testing.T) {
	for _, tc := range []struct {
		action      Action
		wantUse     sandbox.CapabilityUse
		wantSession int
	}{
		{action: AllowOnce, wantUse: sandbox.AuthorizedDelta},
		{action: AllowSession, wantUse: sandbox.AuthorizedDelta, wantSession: 1},
		{action: AllowPersistent, wantUse: sandbox.BaseOnly},
	} {
		t.Run(string(tc.action), func(t *testing.T) {
			approver := &actionApprover{action: tc.action}
			persister := &memoryPersister{}
			engine := &Engine{Approver: approver, Persister: persister}
			got, err := engine.Authorize(context.Background(), Request{
				Review: readyReview(true), Workspace: t.TempDir(), Command: "printf token=secret-value", ReusableArgv: []string{"printf", "token=secret-value"},
			})
			if err != nil || got.Use != tc.wantUse || len(engine.SessionGrants()) != tc.wantSession || len(persister.grants) != 0 {
				t.Fatalf("decision=%+v err=%v session=%d persisted=%d", got, err, len(engine.SessionGrants()), len(persister.grants))
			}
			if !approver.prompt.SuspectedSecret || !strings.Contains(strings.Join(approver.prompt.Warnings, " "), "session grants retain") {
				t.Fatalf("secret prompt=%+v", approver.prompt)
			}
		})
	}
}

func TestBackgroundAndPreserveGrantDimensionsMatchMonotonically(t *testing.T) {
	workspace := t.TempDir()
	baseReq := Request{Review: readyReview(true), Workspace: workspace, Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	id, err := commandIdentity(baseReq)
	if err != nil {
		t.Fatal(err)
	}
	broad := id.grant(baseReq)
	broad.Background = true
	broad.PreserveBackgroundProcesses = true
	engine := &Engine{Source: memoryGrantSource{grants: []Grant{broad}}}
	for _, request := range []Request{
		baseReq,
		func() Request { r := baseReq; r.Background = true; return r }(),
		func() Request { r := baseReq; r.PreserveBackgroundProcesses = true; return r }(),
	} {
		got, err := engine.Authorize(context.Background(), request)
		if err != nil || got.Use != sandbox.AuthorizedDelta {
			t.Fatalf("broad grant did not cover request %+v: %+v err=%v", request, got, err)
		}
	}

	narrow := id.grant(baseReq)
	engine = &Engine{Source: memoryGrantSource{grants: []Grant{narrow}}}
	request := baseReq
	request.Background = true
	request.PreserveBackgroundProcesses = true
	got, err := engine.Authorize(context.Background(), request)
	if err != nil || got.Use != sandbox.BaseOnly {
		t.Fatalf("foreground/non-preserving grant widened: %+v err=%v", got, err)
	}

	prompt := buildPrompt(request, id, nil)
	if !strings.Contains(strings.Join(prompt.Warnings, " "), "survive command completion") {
		t.Fatalf("preserved-process warning=%v", prompt.Warnings)
	}
}

func TestReusableGrantsRejectShellAndPrivilegeWrappers(t *testing.T) {
	for _, command := range []string{"sh -c true", "env printf ok", "sudo printf ok"} {
		t.Run(strings.Fields(command)[0], func(t *testing.T) {
			_, err := commandIdentity(Request{Workspace: t.TempDir(), Command: command, ReusableArgv: strings.Fields(command)})
			if err == nil || !strings.Contains(err.Error(), "wrapper") {
				t.Fatalf("commandIdentity(%q) err=%v", command, err)
			}
		})
	}
}

type failingApprover struct{}

func (failingApprover) ApproveSandboxCapability(context.Context, Prompt) (Action, error) {
	return "", errors.New("frontend disconnected")
}

func TestApprovalFailureAndInvalidAdapterActionFallBackToBase(t *testing.T) {
	req := Request{Review: readyReview(true), Workspace: t.TempDir(), Command: "printf ok", ReusableArgv: []string{"printf", "ok"}}
	for name, approver := range map[string]Approver{
		"failure": failingApprover{},
		"invalid": &actionApprover{action: Action("future_action")},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := (&Engine{Approver: approver}).Authorize(context.Background(), req)
			if err != nil || got.Use != sandbox.BaseOnly || !strings.Contains(got.Diagnostic, "base sandbox") {
				t.Fatalf("decision=%+v err=%v", got, err)
			}
		})
	}
}
