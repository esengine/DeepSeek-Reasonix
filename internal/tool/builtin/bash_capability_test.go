package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

var _ tool.SandboxCapabilityTool = bash{}
var _ tool.DirectSandboxCapabilityInvocation = (*preparedBashInvocation)(nil)

func TestPreparedBashInvocationExposesOnlyStableReusableArgv(t *testing.T) {
	b := bash{
		workDir: t.TempDir(),
		shell:   sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"},
	}
	invocation, err := b.PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": `printf '%s' 'hello world'`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := invocation.SandboxCapabilityRequest()
	want := []string{"printf", "%s", "hello world"}
	if len(request.ReusableArgv) != len(want) {
		t.Fatalf("reusable argv=%v, want %v", request.ReusableArgv, want)
	}
	for index := range want {
		if request.ReusableArgv[index] != want[index] {
			t.Fatalf("reusable argv=%v, want %v", request.ReusableArgv, want)
		}
	}
	request.ReusableArgv[0] = "mutated"
	if got := invocation.SandboxCapabilityRequest().ReusableArgv[0]; got != "printf" {
		t.Fatalf("request mutation leaked into prepared invocation: %q", got)
	}

	for _, command := range []string{
		`printf $(touch denied)`,
		"printf `touch denied`",
		`printf <(generate)`,
		`printf *.pem`,
		`printf ~/secrets`,
		`printf {old,backup}`,
	} {
		t.Run(command, func(t *testing.T) {
			invocation, err := b.PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
				"command": command,
			}))
			if err != nil {
				t.Fatal(err)
			}
			if got := invocation.SandboxCapabilityRequest().ReusableArgv; got != nil {
				t.Fatalf("dynamic command reusable argv=%v, want nil", got)
			}
		})
	}
}

func TestPreparedPowerShellInvocationHasNoReusableArgvWithoutStaticParser(t *testing.T) {
	b := bash{
		workDir: t.TempDir(),
		shell:   sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"},
	}
	invocation, err := b.PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": `& 'C:\Program Files\tool.exe' status`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := invocation.SandboxCapabilityRequest().ReusableArgv; got != nil {
		t.Fatalf("PowerShell reusable argv=%v, want nil until a static parser exists", got)
	}
}

func TestPreparedBashGrantReuseExecutesCanonicalWitnessDirectly(t *testing.T) {
	original := bashPrepareCapabilityDirect
	defer func() { bashPrepareCapabilityDirect = original }()
	canonical, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("printf unavailable")
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(canonical); resolveErr == nil {
		canonical = resolved
	}
	var captured []string
	bashPrepareCapabilityDirect = func(_ context.Context, _ sandbox.CapabilityAssessment, _ sandbox.CapabilityUse, _ sandbox.Shell, _ string, direct []string) sandbox.CapabilityLaunch {
		captured = append([]string(nil), direct...)
		return sandbox.CapabilityLaunch{Argv: append([]string(nil), direct...), Wrapped: true, UsesDelta: true}
	}
	invocation, err := (bash{workDir: t.TempDir()}).PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command":              "printf shadowed",
		"sandbox_capabilities": map[string]any{"network": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	direct := invocation.(tool.DirectSandboxCapabilityInvocation)
	out, err := direct.ExecuteDirect(context.Background(), sandbox.AuthorizedDelta, canonical, []string{canonical, "shadow-safe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) != 2 || captured[0] != canonical || !strings.Contains(out, "shadow-safe") || strings.Contains(out, "shadowed") {
		t.Fatalf("captured=%v out=%q", captured, out)
	}
}

func TestPreparedBashGrantReuseNeverRoutesCanonicalWitnessThroughShellTerminal(t *testing.T) {
	original := bashPrepareCapabilityDirect
	defer func() { bashPrepareCapabilityDirect = original }()
	canonical, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("printf unavailable")
	}
	canonical, _ = filepath.Abs(canonical)
	term := &fakeTerminal{out: "terminal-ran", ok: true}
	bashPrepareCapabilityDirect = func(_ context.Context, _ sandbox.CapabilityAssessment, _ sandbox.CapabilityUse, _ sandbox.Shell, _ string, direct []string) sandbox.CapabilityLaunch {
		return sandbox.CapabilityLaunch{Argv: append([]string(nil), direct...), Wrapped: true, UsesDelta: true}
	}
	invocation, err := (bash{workDir: t.TempDir(), terminal: term}).PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": "printf shadowed", "sandbox_capabilities": map[string]any{"network": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := invocation.(tool.DirectSandboxCapabilityInvocation).ExecuteDirect(
		context.Background(), sandbox.AuthorizedDelta, canonical, []string{canonical, "canonical-ran"},
	)
	if err != nil || !strings.Contains(out, "canonical-ran") || strings.Contains(out, "terminal-ran") {
		t.Fatalf("output=%q err=%v", out, err)
	}
	if len(term.called) != 0 {
		t.Fatalf("authorized delta was routed through shell terminal: %v", term.called)
	}
}

func TestPreparedBashGrantReuseFailureUsesTruthfulBaseTerminalFallback(t *testing.T) {
	original := bashPrepareCapabilityDirect
	defer func() { bashPrepareCapabilityDirect = original }()
	term := &fakeTerminal{out: "base-terminal-ran", ok: true}
	bashPrepareCapabilityDirect = func(_ context.Context, _ sandbox.CapabilityAssessment, _ sandbox.CapabilityUse, sh sandbox.Shell, command string, _ []string) sandbox.CapabilityLaunch {
		argv, wrapped := sandbox.Command(sandbox.Spec{}, sh, command)
		return sandbox.CapabilityLaunch{Argv: argv, Wrapped: wrapped, Diagnostic: "canonical delta unavailable; using base sandbox"}
	}
	invocation, err := (bash{workDir: t.TempDir(), terminal: term}).PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": "printf original", "sandbox_capabilities": map[string]any{"network": true},
	}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := invocation.(tool.DirectSandboxCapabilityInvocation).ExecuteDirect(
		context.Background(), sandbox.AuthorizedDelta, "/canonical/printf", []string{"/canonical/printf", "canonical"},
	)
	if err != nil || !strings.Contains(out, "base-terminal-ran") || !strings.Contains(out, "using base sandbox") {
		t.Fatalf("output=%q err=%v", out, err)
	}
	if len(term.called) != 1 || term.called[0] != "printf original" {
		t.Fatalf("base fallback terminal calls=%v", term.called)
	}
}

func TestBashSchemaAdvertisesBoundedSandboxCapabilities(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type                 string                     `json:"type"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal((bash{}).Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	capability, ok := schema.Properties["sandbox_capabilities"]
	if !ok || capability.Type != "object" || capability.AdditionalProperties == nil || *capability.AdditionalProperties {
		t.Fatalf("sandbox_capabilities schema = %#v", capability)
	}
	for _, name := range []string{"network", "read_paths", "write_paths", "devices", "argv_prefix", "justification"} {
		if _, ok := capability.Properties[name]; !ok {
			t.Fatalf("sandbox_capabilities schema missing %q", name)
		}
	}
	for name, want := range map[string]int{"read_paths": 4, "write_paths": 4, "devices": 4, "argv_prefix": 8} {
		var property struct {
			MaxItems int `json:"maxItems"`
		}
		if err := json.Unmarshal(capability.Properties[name], &property); err != nil {
			t.Fatal(err)
		}
		if property.MaxItems != want {
			t.Fatalf("%s.maxItems = %d, want %d", name, property.MaxItems, want)
		}
	}
}

func TestBashCapabilityOmissionPreservesOutput(t *testing.T) {
	sh := sandbox.ResolveShell("bash", "", nil)
	out, err := (bash{shell: sh, workDir: t.TempDir()}).Execute(context.Background(), argsJSON(t, map[string]any{
		"command": "printf unchanged",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "unchanged" {
		t.Fatalf("output = %q, want byte-identical legacy output", out)
	}
}

func TestBashInvalidCapabilitySoftDeniesAndRunsBaseCommand(t *testing.T) {
	sh := sandbox.ResolveShell("bash", "", nil)
	out, err := (bash{shell: sh, workDir: t.TempDir(), sb: sandbox.Spec{Mode: "off"}}).Execute(context.Background(), argsJSON(t, map[string]any{
		"command": "printf base-ran",
		"sandbox_capabilities": map[string]any{
			"network": true,
			"unknown": true,
		},
	}))
	if err != nil {
		t.Fatalf("base command failed after soft denial: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "base-ran") || !strings.Contains(out, "sandbox capability request was not applied") {
		t.Fatalf("output = %q, want command output plus soft-denial diagnostic", out)
	}
}

func TestPreparedBashInvocationReviewIsDefensiveAndSingleUse(t *testing.T) {
	workspace := t.TempDir()
	invocation, err := (bash{workDir: workspace, sb: sandbox.Spec{Mode: "off"}}).PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": "printf once",
		"sandbox_capabilities": map[string]any{
			"argv_prefix": []string{"printf"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	review := invocation.Review()
	review.ArgvPrefix[0] = "mutated"
	if got := invocation.Review().ArgvPrefix[0]; got != "printf" {
		t.Fatalf("review mutation leaked into invocation: %q", got)
	}
	if _, err := invocation.Execute(context.Background(), sandbox.BaseOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := invocation.Execute(context.Background(), sandbox.BaseOnly); err == nil {
		t.Fatal("prepared invocation executed twice")
	}
}

func TestPreparedBashInvocationAppliesAuthorizedAtomicWriteDelta(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	workspace := t.TempDir()
	external := t.TempDir()
	output := filepath.Join(external, "created.txt")
	b := bash{
		workDir: workspace,
		sb: sandbox.Spec{
			Mode:          "enforce",
			WriteRoots:    []string{workspace},
			Network:       true,
			MinimalWrites: true,
		},
	}
	args := argsJSON(t, map[string]any{
		"command": "printf granted > " + shellSingleQuote(output),
		"sandbox_capabilities": map[string]any{
			"write_paths": []any{map[string]string{
				"identity": string(sandbox.CanonicalAbsolute),
				"path":     external,
			}},
		},
	})
	invocation, err := b.PrepareSandboxInvocation(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if review := invocation.Review(); review.State != sandbox.CapabilityReady {
		t.Skipf("path capabilities unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	out, err := invocation.Execute(context.Background(), sandbox.AuthorizedDelta)
	if err != nil {
		t.Fatalf("authorized command failed: %v (out=%q)", err, out)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "granted" {
		t.Fatalf("output file = %q", got)
	}
}

func TestPreparedBashInvocationReportsAppliedPathStringDeviceDelta(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	b := bash{
		workDir: t.TempDir(),
		sb: sandbox.Spec{
			Mode:          "enforce",
			Network:       true,
			MinimalWrites: true,
		},
	}
	invocation, err := b.PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": "dd if=/dev/null of=/dev/null count=1 status=none",
		"sandbox_capabilities": map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if review := invocation.Review(); review.State != sandbox.CapabilityReady {
		t.Skipf("device capabilities unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	out, err := invocation.Execute(context.Background(), sandbox.AuthorizedDelta)
	if err != nil {
		t.Fatalf("authorized device command failed: %v (out=%q)", err, out)
	}
	for _, want := range []string{
		"requested=true", "supported=true", "prepared=true", "applied=true",
		sandbox.CapabilityMaterializationPathStringDevBind.Disclosure(),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing authoritative device diagnostic %q", out, want)
		}
	}
}

func TestPreparedForegroundBashCancellationKeepsMissingWitnessUnknown(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	b := bash{
		workDir: t.TempDir(),
		sb:      sandbox.Spec{Mode: "enforce", Network: true, MinimalWrites: true},
	}
	invocation, err := b.PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": "sleep 30",
		"sandbox_capabilities": map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if review := invocation.Review(); review.State != sandbox.CapabilityReady {
		t.Skipf("device capabilities unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := invocation.Execute(canceled, sandbox.AuthorizedDelta)
	if err == nil {
		t.Fatalf("pre-canceled foreground execution unexpectedly succeeded: output=%q", out)
	}
	if !strings.Contains(out, "prepared=true") || !strings.Contains(out, "applied=unknown") {
		t.Fatalf("canceled foreground output = %q, missing final witness must remain unknown", out)
	}
}

func TestPreparedForegroundBashExternallySignaledWrapperIsInterrupted(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	b := bash{workDir: t.TempDir(), sb: sandbox.Spec{Mode: "enforce", Network: true, MinimalWrites: true}}
	invocation, err := b.PrepareSandboxInvocation(context.Background(), argsJSON(t, map[string]any{
		"command": `kill -TERM "$PPID"; exit 0`,
		"sandbox_capabilities": map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if review := invocation.Review(); review.State != sandbox.CapabilityReady {
		t.Skipf("device capabilities unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	out, err := invocation.Execute(context.Background(), sandbox.AuthorizedDelta)
	if err == nil {
		t.Fatalf("externally signaled Bubblewrap wrapper unexpectedly succeeded: output=%q", out)
	}
	if !strings.Contains(out, "prepared=true") || !strings.Contains(out, "applied=unknown") {
		t.Fatalf("signaled wrapper output = %q, missing final witness must remain unknown", out)
	}
}

func TestBashCapabilityExecutionOutcomeKeepsOrdinaryNonzeroCompleted(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Run(); err == nil {
		t.Fatal("test command unexpectedly exited zero")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if outcome := bashCapabilityExecutionOutcome(canceled, cmd); outcome != sandbox.CapabilityExecutionCompleted {
		t.Fatalf("exit 7 outcome = %v, want completed", outcome)
	}
}

func TestPreparedBackgroundBashReportsWitnessedApplicationAfterNonzeroExit(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), manager), "capability-session")
	b := bash{
		workDir: t.TempDir(),
		sb:      sandbox.Spec{Mode: "enforce", Network: true, MinimalWrites: true},
	}
	invocation, err := b.PrepareSandboxInvocation(ctx, argsJSON(t, map[string]any{
		"command":           "exit 7",
		"run_in_background": true,
		"sandbox_capabilities": map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if review := invocation.Review(); review.State != sandbox.CapabilityReady {
		t.Skipf("device capabilities unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	out, err := invocation.Execute(ctx, sandbox.AuthorizedDelta)
	if err != nil {
		t.Fatalf("background start failed: %v (out=%q)", err, out)
	}
	jobID := backgroundJobIDFromStartOutput(t, out)
	results := manager.WaitForSession(context.Background(), "capability-session", []string{jobID}, 5)
	if len(results) != 1 || results[0].Status != jobs.Failed {
		t.Fatalf("results = %#v, want one failed user command", results)
	}
	if !strings.Contains(results[0].Output, "prepared=true") || !strings.Contains(results[0].Output, "applied=true") {
		t.Fatalf("background output = %q, want witnessed applied authority despite exit 7", results[0].Output)
	}
}

func TestPreparedBackgroundBashCancellationNeverClaimsNotAppliedWithoutProof(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("OS sandbox unavailable")
	}
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), manager), "canceled-capability-session")
	b := bash{workDir: t.TempDir(), sb: sandbox.Spec{Mode: "enforce", Network: true, MinimalWrites: true}}
	invocation, err := b.PrepareSandboxInvocation(ctx, argsJSON(t, map[string]any{
		"command": "sleep 30", "run_in_background": true,
		"sandbox_capabilities": map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if review := invocation.Review(); review.State != sandbox.CapabilityReady {
		t.Skipf("device capabilities unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	out, err := invocation.Execute(ctx, sandbox.AuthorizedDelta)
	if err != nil {
		t.Fatalf("background start failed: %v (out=%q)", err, out)
	}
	jobID := backgroundJobIDFromStartOutput(t, out)
	if !manager.KillForSession("canceled-capability-session", jobID) {
		t.Fatalf("failed to cancel background job %q", jobID)
	}
	results := manager.WaitForSession(context.Background(), "canceled-capability-session", []string{jobID}, 5)
	if len(results) != 1 || results[0].Status != jobs.Killed {
		t.Fatalf("results = %#v, want one killed job", results)
	}
	if strings.Contains(results[0].Output, "applied=false") {
		t.Fatalf("canceled background output = %q, cancellation cannot prove authority was never active", results[0].Output)
	}
	if !strings.Contains(results[0].Output, "applied=unknown") && !strings.Contains(results[0].Output, "applied=true") {
		t.Fatalf("canceled background output = %q, want conservative unknown or witnessed true", results[0].Output)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
