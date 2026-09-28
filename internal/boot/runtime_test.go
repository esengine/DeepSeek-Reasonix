package boot

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func TestBuildRuntimeDisablesImplicitSkillInvocation(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	configPath := filepath.Join(dir, "reasonix.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}
	content = append(content, []byte("\n[skills]\ndisable_implicit_invocation = true\n")...)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write skills config: %v", err)
	}
	res := buildRuntimeFixture(t)
	if res.Controller.ImplicitSkillInvocationEnabled() {
		t.Fatal("controller should disable implicit skill invocation")
	}
	if res.Assembly == nil || res.Assembly.ImplicitSkillInvocation {
		t.Fatal("reused assembly should record implicit skill invocation as disabled")
	}
	if strings.Contains(res.Snapshot.SystemPrompt(), "One-liner index") {
		t.Fatal("skill index should not be provider-visible when implicit invocation is disabled")
	}
	for _, entry := range res.Controller.ToolContractEntries() {
		switch entry.Name {
		case "run_skill", "read_skill", "read_only_skill", "install_skill":
			t.Fatalf("skill tool %q should not be exposed to the model", entry.Name)
		}
	}
	if _, ok := res.Controller.RunSkill("/explore inspect"); !ok {
		t.Fatal("explicit /skill invocation should remain available")
	}
}

func TestRebuildFromForceFullRebuildRefreshesSkillPolicy(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	configPath := filepath.Join(dir, "reasonix.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read fixture config: %v", err)
	}
	content = append(content, []byte("\n[skills]\ndisable_implicit_invocation = true\n")...)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write disabled skill policy: %v", err)
	}
	previous, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("initial BuildRuntime: %v", err)
	}
	if previous.Controller.ImplicitSkillInvocationEnabled() {
		t.Fatal("initial controller should disable implicit skill invocation")
	}
	content = bytes.Replace(content, []byte("disable_implicit_invocation = true"), []byte("disable_implicit_invocation = false"), 1)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write enabled skill policy: %v", err)
	}
	res, err := RebuildFrom(context.Background(), previous, Options{
		RuntimeReload: RuntimeReload{ForceFullRebuild: true},
	})
	if err != nil {
		previous.Controller.Close()
		t.Fatalf("forced RebuildFrom: %v", err)
	}
	t.Cleanup(func() {
		res.Controller.Close()
	})
	if res.Controller == previous.Controller {
		t.Fatal("forced rebuild reused the previous controller")
	}
	if !res.Controller.ImplicitSkillInvocationEnabled() {
		t.Fatal("forced rebuild did not refresh the enabled skill policy")
	}
	previous.Controller.Close()
}

// writeRuntimeFixture writes the minimal deterministic config the runtime
// tests share: a resolvable model, a fixed base system prompt, and the
// environment probe section disabled (it embeds machine-specific data).
func writeRuntimeFixture(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)
	approveWorkspace(t, dir)
}

// buildRuntimeFixture builds one runtime against the fixture and registers
// its controller for cleanup.
func buildRuntimeFixture(t *testing.T) *BuildResult {
	t.Helper()
	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	if res.Controller == nil {
		t.Fatal("BuildRuntime returned a nil controller")
	}
	t.Cleanup(res.Controller.Close)
	return res
}

// TestBuildRuntimeSnapshotMatchesController pins the stage-3a contract: the
// kernel snapshot mirrors exactly what the build wired — same system prompt,
// same provider-visible tool contract — its cache fingerprint is stable
// across identical builds, and generations increase monotonically.
func TestBuildRuntimeSnapshotMatchesController(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)

	first := buildRuntimeFixture(t)
	if first.Snapshot == nil {
		t.Fatal("BuildRuntime returned a nil snapshot")
	}
	if first.Runtime == nil {
		t.Fatal("BuildRuntime returned a nil runtime set")
	}
	if first.Runtime.Len() != 0 {
		t.Fatalf("stage-3a runtime set holds %d closers, want 0 (sidecars arrive in stage 5)", first.Runtime.Len())
	}
	if first.Snapshot.Generation() == 0 {
		t.Fatal("snapshot generation = 0, want the counter to start at 1")
	}

	// The snapshot's system prompt is exactly the controller's system
	// message.
	if got, want := first.Snapshot.SystemPrompt(), systemMessage(first.Controller.History()); got != want {
		t.Fatalf("snapshot system prompt != controller system message\n got: %q\nwant: %q", got, want)
	}

	// The snapshot's tool schemas are exactly the controller's tool contract,
	// entry for entry.
	entries := first.Controller.ToolContractEntries()
	schemas := first.Snapshot.ToolSchemas()
	if len(entries) == 0 {
		t.Fatal("BuildRuntime registered no tools")
	}
	if len(schemas) != len(entries) {
		t.Fatalf("snapshot holds %d tool schemas, controller contract has %d", len(schemas), len(entries))
	}
	for i, e := range entries {
		s := schemas[i]
		if s.Name != e.Name || s.Description != e.Description || string(s.Parameters) != string(e.Schema) {
			t.Fatalf("tool schema %d = (%q, %.40q, %.40q), want (%q, %.40q, %.40q)",
				i, s.Name, s.Description, s.Parameters, e.Name, e.Description, e.Schema)
		}
	}

	// A clean fixture records no diagnostics.
	if diags := first.Snapshot.Diagnostics(); len(diags) != 0 {
		t.Fatalf("snapshot diagnostics = %v, want none", diags)
	}

	// An identical second build reproduces the same CacheHash (the
	// provider-cache fingerprint) at a higher generation.
	second := buildRuntimeFixture(t)
	if second.Snapshot == nil {
		t.Fatal("second BuildRuntime returned a nil snapshot")
	}
	if got, want := second.Snapshot.CacheHash(), first.Snapshot.CacheHash(); got != want {
		t.Fatalf("CacheHash drifted across identical builds: %s vs %s", got, want)
	}
	if got, before := second.Snapshot.Generation(), first.Snapshot.Generation(); got <= before {
		t.Fatalf("generation did not increase across builds: %d then %d", before, got)
	}
}

// TestRebuildMigratesSessionState drives the success path: an old controller
// with a conversation, session grants, and the session axes set rebuilds into
// a replacement that continues the same session file with everything carried
// — while the old controller stays fully usable.
func TestRebuildMigratesSessionState(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)

	old, err := BuildRuntime(context.Background(), withTestSession(t, Options{}))
	if err != nil {
		t.Fatalf("BuildRuntime v3: %v", err)
	}
	t.Cleanup(old.Controller.Close)
	oldCtrl := old.Controller

	// Pin a v3 session and seed a conversation plus the session axes the
	// rebuild must carry.
	oldCtrl.EnsureSessionPath()
	prevRef, ok := oldCtrl.SessionRef()
	if !ok {
		t.Fatal("old controller pinned no v3 session")
	}
	oldCtrl.AdoptHistory([]provider.Message{
		{Role: provider.RoleSystem, Content: systemMessage(oldCtrl.History())},
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "hi there"},
	}, "")
	oldCtrl.SetToolApprovalMode(control.ToolApprovalYolo)
	oldCtrl.SetPlanMode(true)
	oldCtrl.SetGoal("ship the kernel")
	oldCtrl.RestoreSessionAuthorizations(control.SessionAuthorizations{
		Grants:                   []string{"bash(go test ./...)"},
		PlanModeReadOnlyCommands: []string{"git status"},
	})
	oldHistory := oldCtrl.History()

	res, err := Rebuild(context.Background(), oldCtrl, Options{})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if res.Snapshot == nil {
		t.Fatal("Rebuild returned a nil snapshot")
	}
	if res.Snapshot.Generation() <= old.Snapshot.Generation() {
		t.Fatalf("generation did not increase: old %d, new %d", old.Snapshot.Generation(), res.Snapshot.Generation())
	}
	defer res.Controller.Close()

	// The conversation continues on the same immutable v3 identity with identical
	// messages (the fixture rebuild produces the same system prompt, so the
	// splice is invisible here).
	gotRef, ok := res.Controller.SessionRef()
	if !ok || gotRef != prevRef {
		t.Fatalf("session ref = %+v, want continued %+v", gotRef, prevRef)
	}
	if got := res.Controller.SessionPath(); got != "" {
		t.Fatalf("rebuilt v3 controller retained legacy path %q", got)
	}
	newHistory := res.Controller.History()
	if len(newHistory) != len(oldHistory) {
		t.Fatalf("new history has %d messages, want %d", len(newHistory), len(oldHistory))
	}
	for i := range oldHistory {
		if newHistory[i].Role != oldHistory[i].Role || newHistory[i].Content != oldHistory[i].Content {
			t.Fatalf("history[%d] = (%s, %q), want (%s, %q)",
				i, newHistory[i].Role, newHistory[i].Content, oldHistory[i].Role, oldHistory[i].Content)
		}
	}

	// Session axes migrated.
	if got := res.Controller.ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("tool approval mode = %q, want %q", got, control.ToolApprovalYolo)
	}
	if !res.Controller.PlanMode() {
		t.Fatal("plan mode did not migrate")
	}
	if got := res.Controller.Goal(); got != "ship the kernel" {
		t.Fatalf("goal = %q, want migrated %q", got, "ship the kernel")
	}
	auth := res.Controller.SessionAuthorizations()
	if !slices.Contains(auth.Grants, "bash(go test ./...)") {
		t.Fatalf("session grants = %v, want the migrated grant", auth.Grants)
	}
	if !slices.Contains(auth.PlanModeReadOnlyCommands, "git status") {
		t.Fatalf("plan-mode trust = %v, want the migrated prefix", auth.PlanModeReadOnlyCommands)
	}

	// old keeps working: history intact, runtime set untouched, close clean.
	if got := len(oldCtrl.History()); got != len(oldHistory) {
		t.Fatalf("old controller history changed during rebuild: %d, want %d", got, len(oldHistory))
	}
	if old.Runtime.Closed() {
		t.Fatal("Rebuild closed the old runtime set")
	}
	oldCtrl.Close()
}

func TestRebuildKeepsLegacySessionNativeWithHostService(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)

	old, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime legacy: %v", err)
	}
	t.Cleanup(old.Controller.Close)
	old.Controller.EnsureSessionPath()
	old.Controller.AdoptHistory([]provider.Message{
		{Role: provider.RoleSystem, Content: systemMessage(old.Controller.History())},
		{Role: provider.RoleUser, Content: "legacy history"},
	}, old.Controller.SessionPath())
	if err := old.Controller.Snapshot(); err != nil {
		t.Fatalf("Snapshot legacy: %v", err)
	}

	workspace := filepath.Join(dir, "workspace")
	storeRoot := filepath.Join(dir, "desktop-sessions-v5", "by-id")
	service, err := session.NewService("local", session.NewFilesystemPersistence(storeRoot))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })

	rebuilt, err := Rebuild(context.Background(), old.Controller, Options{
		SessionService: service,
		SessionCreateOptions: session.CreateOptions{
			CWD: workspace, Origin: session.SessionOriginLegacyImport,
		},
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	t.Cleanup(rebuilt.Controller.Close)
	if _, ok := rebuilt.Controller.SessionRef(); ok || !rebuilt.Controller.NativeLegacySession() {
		t.Fatal("rebuild converted historical storage")
	}
	if rebuilt.Controller.SessionPath() != old.Controller.SessionPath() || rebuilt.Controller.SessionService() != service {
		t.Fatal("rebuild lost native path or current-store service")
	}
	if got := rebuilt.Controller.History(); len(got) != 2 || got[1].Content != "legacy history" {
		t.Fatalf("rebuild lost history: %+v", got)
	}
	if entries, err := os.ReadDir(storeRoot); err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatalf("rebuild created canonical history: %v %v", entries, err)
	}
	other, err := session.NewService("local", session.NewFilesystemPersistence(filepath.Join(dir, "other-root")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Shutdown(context.Background()) })
	if err := control.ActivateControllerReplacement(old.Controller, rebuilt.Controller); err != nil {
		t.Fatal(err)
	}
	next, err := Rebuild(context.Background(), rebuilt.Controller, Options{SessionService: other})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(next.Controller.Close)
	if next.Controller.SessionService() != service || next.Controller.SessionCreationService() != service || next.Controller.SessionPath() != rebuilt.Controller.SessionPath() {
		t.Fatal("second rebuild replaced the authoritative store or creation service")
	}
}

func TestNativeRebuildPreservesSelectedDAGHead(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	old := buildRuntimeFixture(t)
	t.Cleanup(old.Controller.Close)
	path := filepath.Join(dir, "selected.jsonl")
	s := agent.NewSession(systemMessage(old.Controller.History()))
	s.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "main answer"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ForkHead(path, s.Snapshot()[1].ID, agent.HeadKindFork, "alternate"); err != nil {
		t.Fatal(err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "alternate answer"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	selected, err := agent.LoadSessionHeadReadOnly(path, agent.SessionMainHead)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Controller.ResumeNativeSession(selected, path); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Rebuild(t.Context(), old.Controller, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rebuilt.Controller.Close)
	head, ok := rebuilt.Controller.SessionHead()
	if !ok || head.HeadID != agent.SessionMainHead {
		t.Fatalf("rebuild switched head: %+v", head)
	}
	if err := rebuilt.Controller.Snapshot(); err != nil {
		t.Fatal(err)
	}
	reopened, err := agent.LoadSessionHeadReadOnly(path, agent.SessionMainHead)
	if err != nil {
		t.Fatal(err)
	}
	if history := reopened.Snapshot(); history[len(history)-1].Content != "main answer" {
		t.Fatalf("rebuild overwrote selected history: %+v", history)
	}
	other, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if history := other.Snapshot(); history[len(history)-1].Content != "alternate answer" {
		t.Fatalf("rebuild changed default head: %+v", history)
	}
}

// TestRebuildCarriesGoalWithoutSessionPath covers the in-memory fallback: an
// old controller that never pinned a session file has no Goal sidecar to
// restore, so the running Goal migrates from memory.
func TestRebuildCarriesGoalWithoutSessionPath(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)

	old := buildRuntimeFixture(t)
	oldCtrl := old.Controller
	if got := oldCtrl.SessionPath(); got != "" {
		t.Fatalf("fresh controller session path = %q, want empty", got)
	}
	oldCtrl.SetGoal("ship the kernel")

	res, err := Rebuild(context.Background(), oldCtrl, Options{})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	defer res.Controller.Close()
	if got := res.Controller.Goal(); got != "ship the kernel" {
		t.Fatalf("goal = %q, want seeded %q", got, "ship the kernel")
	}
}

// TestRebuildFailureKeepsOldController drives the fail-atomic path: the
// replacement build fails (unknown model), the error propagates, and the old
// controller is untouched.
func TestRebuildFailureKeepsOldController(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)

	old := buildRuntimeFixture(t)
	oldCtrl := old.Controller
	oldCtrl.EnsureSessionPath()
	prevPath := oldCtrl.SessionPath()
	oldCtrl.AdoptHistory([]provider.Message{
		{Role: provider.RoleSystem, Content: systemMessage(oldCtrl.History())},
		{Role: provider.RoleUser, Content: "hello"},
	}, prevPath)

	res, err := Rebuild(context.Background(), oldCtrl, Options{Model: "definitely-unknown-model"})
	if err == nil {
		t.Fatal("Rebuild succeeded, want ErrUnknownModel")
	}
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("Rebuild error = %v, want ErrUnknownModel", err)
	}
	if res != nil {
		t.Fatalf("Rebuild returned a partial result on failure: %+v", res)
	}

	// old is fully usable: history and path intact, runtime set untouched,
	// close clean.
	if got := len(oldCtrl.History()); got != 2 {
		t.Fatalf("old history = %d messages, want 2", got)
	}
	if got := oldCtrl.SessionPath(); got != prevPath {
		t.Fatalf("old session path = %q, want %q", got, prevPath)
	}
	if old.Runtime.Closed() {
		t.Fatal("Rebuild closed the old runtime set on failure")
	}
	oldCtrl.Close()
}

// TestSpliceFreshSystemPrompt pins the system-message splice used to refresh
// the profile contract on a continued conversation.
func TestSpliceFreshSystemPrompt(t *testing.T) {
	fresh := []provider.Message{{Role: provider.RoleSystem, Content: "new prompt"}}
	t.Run("replaces the carried system message", func(t *testing.T) {
		carried := []provider.Message{
			{Role: provider.RoleSystem, Content: "old prompt"},
			{Role: provider.RoleUser, Content: "hi"},
		}
		got := spliceFreshSystemPrompt(carried, fresh)
		if len(got) != 2 || got[0].Content != "new prompt" || got[1].Content != "hi" {
			t.Fatalf("splice = %+v", got)
		}
	})
	t.Run("prepends when the carried conversation has none", func(t *testing.T) {
		carried := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
		got := spliceFreshSystemPrompt(carried, fresh)
		if len(got) != 2 || got[0].Role != provider.RoleSystem || got[1].Content != "hi" {
			t.Fatalf("splice = %+v", got)
		}
	})
	t.Run("no fresh system message leaves the conversation untouched", func(t *testing.T) {
		carried := []provider.Message{{Role: provider.RoleUser, Content: "hi"}}
		got := spliceFreshSystemPrompt(carried, []provider.Message{{Role: provider.RoleUser, Content: "yo"}})
		if len(got) != 1 || got[0].Content != "hi" {
			t.Fatalf("splice = %+v", got)
		}
	})
	t.Run("does not alias the input slice", func(t *testing.T) {
		carried := []provider.Message{{Role: provider.RoleSystem, Content: "old prompt"}}
		got := spliceFreshSystemPrompt(carried, fresh)
		got[0].Content = "mutated"
		if carried[0].Content != "old prompt" {
			t.Fatal("splice wrote through to the caller's slice")
		}
	})
}
