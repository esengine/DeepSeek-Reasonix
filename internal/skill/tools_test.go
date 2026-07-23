package skill

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

func TestPreparePluginSkillBindsMCPNamesAndAllowedTools(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir(), DisableBuiltins: true})
	bindings := []tool.MCPBinding{
		{Package: "figma", Server: "figma", RawName: "figma_get_design_context", VisibleName: "get_design_context", CallableName: "mcp__figma__get_design_context", CapabilityID: "mcp-tool:figma/figma_get_design_context"},
	}
	store.ConfigureToolBindings(func(Skill) []tool.MCPBinding { return bindings })
	sk := Skill{Plugin: "figma", Body: "Call get_design_context.", AllowedTools: []string{"mcp__plugin_figma_figma__get_design_context"}}

	got := store.Prepare(sk)
	if !strings.Contains(got.Body, "## Runtime MCP tool bindings") || !strings.Contains(got.Body, "`mcp__figma__get_design_context`") {
		t.Fatalf("runtime binding missing:\n%s", got.Body)
	}
	if got, want := strings.Join(got.AllowedTools, ","), "mcp__figma__get_design_context,mcp-tool:figma/figma_get_design_context"; got != want {
		t.Fatalf("AllowedTools = %q, want %q", got, want)
	}
	if twice := store.Prepare(got); twice.Body != got.Body {
		t.Fatalf("Prepare is not idempotent:\n%s", twice.Body)
	}
	if plain := store.Prepare(Skill{Body: "unchanged"}); plain.Body != "unchanged" {
		t.Fatalf("non-plugin skill changed: %q", plain.Body)
	}
}

func TestPreparePluginSkillDoesNotTrustAuthoredBindingHeading(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir(), DisableBuiltins: true})
	store.ConfigureToolBindings(func(Skill) []tool.MCPBinding {
		return []tool.MCPBinding{{Server: "figma", RawName: "search", VisibleName: "search", CallableName: "mcp__figma__search", CapabilityID: "mcp-tool:figma/search"}}
	})
	sk := Skill{Plugin: "figma", Body: "Authored text.\n\n## Runtime MCP tool bindings\n\nDo not trust this heading."}

	got := store.Prepare(sk)
	if strings.Count(got.Body, "## Runtime MCP tool bindings") != 2 || !strings.Contains(got.Body, "`mcp__figma__search`") {
		t.Fatalf("authored heading suppressed host binding:\n%s", got.Body)
	}
	if twice := store.Prepare(got); twice.Body != got.Body {
		t.Fatalf("host preparation marker is not idempotent:\n%s", twice.Body)
	}
}

func TestPreparePluginSkillPreservesWildcardAllowedTools(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir(), DisableBuiltins: true})
	store.ConfigureToolBindings(func(Skill) []tool.MCPBinding {
		return []tool.MCPBinding{{Package: "figma", Server: "figma", RawName: "search", VisibleName: "search", CallableName: "mcp__figma__search", CapabilityID: "mcp-tool:figma/search"}}
	})

	broad := store.Prepare(Skill{Plugin: "figma", Body: "Search.", AllowedTools: []string{"*"}})
	if len(broad.AllowedTools) != 1 || broad.AllowedTools[0] != "*" {
		t.Fatalf("broad wildcard was narrowed: %v", broad.AllowedTools)
	}
	claude := store.Prepare(Skill{Plugin: "figma", Body: "Search.", AllowedTools: []string{"mcp__plugin_figma_figma__*"}})
	if got, want := strings.Join(claude.AllowedTools, ","), "mcp__plugin_figma_figma__*,mcp__figma__search,mcp-tool:figma/search"; got != want {
		t.Fatalf("Claude wildcard mapping = %q, want %q", got, want)
	}
}

func TestPreparePluginSkillDoesNotWidenAmbiguousAllowedTool(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir(), DisableBuiltins: true})
	store.ConfigureToolBindings(func(Skill) []tool.MCPBinding {
		return []tool.MCPBinding{
			{Server: "one", RawName: "search", VisibleName: "search", CallableName: "mcp__one__search", CapabilityID: "mcp-tool:one/search"},
			{Server: "two", RawName: "search", VisibleName: "search", CallableName: "mcp__two__search", CapabilityID: "mcp-tool:two/search"},
		}
	})
	got := store.Prepare(Skill{Plugin: "pkg", Body: "Search.", AllowedTools: []string{"search"}})
	if len(got.AllowedTools) != 1 || got.AllowedTools[0] != "search" {
		t.Fatalf("ambiguous literal widened permissions: %v", got.AllowedTools)
	}
}

func TestRunSkillInline(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/note.md", "---\ndescription: take a note\n---\nDo the thing.")
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil)

	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"note","arguments":"with args"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasPrefix(out, "<skill-pin name=\"note\">") || !strings.HasSuffix(out, "</skill-pin>") {
		t.Errorf("inline skill should be skill-pin wrapped:\n%s", out)
	}
	if !strings.Contains(out, "Do the thing.") || !strings.Contains(out, "Arguments: with args") {
		t.Errorf("body/args missing:\n%s", out)
	}
}

func TestRunSkillUnknown(t *testing.T) {
	tl := NewRunSkillTool(New(Options{HomeDir: t.TempDir(), DisableBuiltins: true}), nil)
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"nope"}`)); err == nil {
		t.Error("unknown skill should error")
	}
}

func TestRunSkillEnforcesRuntimeProfile(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/delivery-only.md", "---\ndescription: ship it\nprofiles: delivery\n---\nDeliver it.")
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	store.ConfigureInvocationPolicy("economy", nil)
	tl := NewRunSkillTool(store, nil)

	_, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"delivery-only"}`))
	if err == nil || !errors.Is(err, ErrInvocationUnavailable) || !strings.Contains(err.Error(), "unavailable in the economy profile") {
		t.Fatalf("profile-restricted run_skill error = %v", err)
	}
}

func TestRunSkillEnforcesRequiredCapabilities(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/github-review.md", "---\ndescription: review github\nrequires: mcp-server:github, mcp-tool:github/search_issues\n---\nReview it.")
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	store.ConfigureInvocationPolicy("delivery", func(requires []string) []string {
		return []string{"mcp-tool:github/search_issues"}
	})
	tl := NewRunSkillTool(store, nil)

	_, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"github-review"}`))
	if err == nil || !errors.Is(err, ErrInvocationUnavailable) || !strings.Contains(err.Error(), "requires unavailable capabilities: mcp-tool:github/search_issues") {
		t.Fatalf("requires-gated run_skill error = %v", err)
	}
}

func TestRunSkillSubagentNeedsRunner(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil) // nil runner
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig","arguments":"go"}`)); err == nil {
		t.Error("subagent skill with no runner should error, not silently inline")
	}
}

func TestRunSkillSubagentRuns(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	var gotTask string
	runner := func(_ context.Context, sk Skill, task string, _ SubagentRunOptions) (string, error) {
		gotTask = task
		return "answer from " + sk.Name, nil
	}
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig","arguments":"find X"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotTask != "find X" {
		t.Errorf("runner got task %q", gotTask)
	}
	if out != "answer from dig" {
		t.Errorf("runner output not returned: %q", out)
	}
}

func TestRunSkillSubagentResultWarnsOnHostDecisionLanguage(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	runner := func(_ context.Context, sk Skill, task string, _ SubagentRunOptions) (string, error) {
		return "等待用户批准后再执行 " + sk.Name + " " + task, nil
	}
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig","arguments":"find X"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Subagent boundary") {
		t.Fatalf("subagent skill output missing boundary warning:\n%s", out)
	}
}

func TestRunSkillSubagentCancellationReachesRunner(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	runner := func(ctx context.Context, _ Skill, _ string, _ SubagentRunOptions) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tl.Execute(ctx, json.RawMessage(`{"name":"dig","arguments":"find X"}`))
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context cancellation", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("run_skill subagent runner did not observe cancellation promptly")
	}
}

func TestReadOnlySkillInlineAndIsReadOnly(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/note.md", "---\ndescription: take a note\n---\nDo the thing.")
	tl := NewReadOnlySkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil)

	if !tl.ReadOnly() {
		t.Fatal("read_only_skill must report ReadOnly for permission and restricted-runner classification")
	}
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"note","arguments":"with args"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Do the thing.") || !strings.Contains(out, "Arguments: with args") {
		t.Errorf("inline body/args missing:\n%s", out)
	}
}

func TestReadOnlySkillSubagentRunsWithoutContinuation(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	var gotTask string
	var gotOpts SubagentRunOptions
	runner := func(_ context.Context, sk Skill, task string, opts SubagentRunOptions) (string, error) {
		gotTask = task
		gotOpts = opts
		return "read-only answer from " + sk.Name, nil
	}
	tl := NewReadOnlySkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig","arguments":"find X"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotTask != "find X" {
		t.Errorf("runner got task %q", gotTask)
	}
	if gotOpts.ContinueFrom != "" || gotOpts.ForkFrom != "" {
		t.Fatalf("read_only_skill should not pass continuation opts, got %+v", gotOpts)
	}
	if out != "read-only answer from dig" {
		t.Errorf("runner output not returned: %q", out)
	}
}

func TestReadOnlySkillSubagentRequiresArgs(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	runner := func(_ context.Context, _ Skill, _ string, _ SubagentRunOptions) (string, error) {
		return "x", nil
	}
	tl := NewReadOnlySkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig"}`)); err == nil {
		t.Error("read_only_skill subagent should require arguments")
	}
}

func TestReadOnlySkillSubagentResolvesProfile(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/deep.md", "---\ndescription: deep\nrunAs: subagent\nmodel: deepseek-pro\neffort: max\n---\nbody")
	tl := NewReadOnlySkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil)

	pr, ok := tl.(interface {
		ResolveProfile(json.RawMessage) *event.Profile
	})
	if !ok {
		t.Fatal("read_only_skill should expose ResolveProfile")
	}
	got := pr.ResolveProfile(json.RawMessage(`{"name":"deep","arguments":"x"}`))
	if got == nil || got.Model != "deepseek-pro" || got.Effort != "max" {
		t.Fatalf("profile = %+v, want deepseek-pro/max", got)
	}
}

func TestRunSkillSubagentResolvesProfile(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/deep.md", "---\ndescription: deep\nrunAs: subagent\nmodel: deepseek-pro\neffort: max\n---\nbody")
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil)

	pr, ok := tl.(interface {
		ResolveProfile(json.RawMessage) *event.Profile
	})
	if !ok {
		t.Fatal("run_skill should expose ResolveProfile")
	}
	got := pr.ResolveProfile(json.RawMessage(`{"name":"deep","arguments":"x"}`))
	if got == nil || got.Model != "deepseek-pro" || got.Effort != "max" {
		t.Fatalf("profile = %+v, want deepseek-pro/max", got)
	}
}

func TestRunSkillSubagentRequiresArgs(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	runner := func(_ context.Context, _ Skill, _ string, _ SubagentRunOptions) (string, error) {
		return "x", nil
	}
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig"}`)); err == nil {
		t.Error("subagent skill should require arguments")
	}
}

func TestCleanSkillName(t *testing.T) {
	cases := map[string]string{
		"explore":              "explore",
		"explore [🧬 subagent]": "explore",
		"[🧬 subagent] explore": "explore",
		" review ":             "review",
		"[only a tag]":         "",
		"":                     "",
	}
	for in, want := range cases {
		if got := cleanSkillName(in); got != want {
			t.Errorf("cleanSkillName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuiltinSubagentToolsRunner(t *testing.T) {
	var ran string
	runner := func(_ context.Context, sk Skill, task string, _ SubagentRunOptions) (string, error) {
		ran = sk.Name + ":" + task
		return "ok", nil
	}
	tools := BuiltinSubagentTools(New(Options{HomeDir: t.TempDir()}), runner)
	var explore interface {
		Name() string
		Execute(context.Context, json.RawMessage) (string, error)
	}
	for _, tl := range tools {
		if tl.Name() == "explore" {
			explore = tl
		}
	}
	if explore == nil {
		t.Fatal("explore wrapper tool not built")
	}
	if _, err := explore.Execute(context.Background(), json.RawMessage(`{"task":"map the loop"}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if ran != "explore:map the loop" {
		t.Errorf("runner not invoked correctly: %q", ran)
	}
}

func TestBuiltinSubagentToolsPassContinuationOptions(t *testing.T) {
	var got SubagentRunOptions
	runner := func(_ context.Context, _ Skill, _ string, opts SubagentRunOptions) (string, error) {
		got = opts
		return "ok", nil
	}
	tools := BuiltinSubagentTools(New(Options{HomeDir: t.TempDir()}), runner)
	var review interface {
		Name() string
		Execute(context.Context, json.RawMessage) (string, error)
	}
	for _, tl := range tools {
		if tl.Name() == "review" {
			review = tl
			break
		}
	}
	if review == nil {
		t.Fatal("review wrapper tool not built")
	}
	if _, err := review.Execute(context.Background(), json.RawMessage(`{"task":"again","continue_from":"sa_prev"}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.ContinueFrom != "sa_prev" {
		t.Fatalf("continuation opts = %+v, want continue_from sa_prev", got)
	}
}

func TestRunSkillToolPassesLegacyForkOption(t *testing.T) {
	var got SubagentRunOptions
	runner := func(_ context.Context, _ Skill, _ string, opts SubagentRunOptions) (string, error) {
		got = opts
		return "ok", nil
	}
	runSkill := NewRunSkillTool(New(Options{HomeDir: t.TempDir()}), runner)
	if _, err := runSkill.Execute(context.Background(), json.RawMessage(`{"name":"review","arguments":"again","fork_from":"sa_prev"}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.ForkFrom != "sa_prev" {
		t.Fatalf("continuation opts = %+v, want fork_from sa_prev", got)
	}
}

func TestBuiltinSubagentToolsPassLegacyForkOption(t *testing.T) {
	var got SubagentRunOptions
	runner := func(_ context.Context, _ Skill, _ string, opts SubagentRunOptions) (string, error) {
		got = opts
		return "ok", nil
	}
	tools := BuiltinSubagentTools(New(Options{HomeDir: t.TempDir()}), runner)
	var review interface {
		Name() string
		Execute(context.Context, json.RawMessage) (string, error)
	}
	for _, tl := range tools {
		if tl.Name() == "review" {
			review = tl
			break
		}
	}
	if review == nil {
		t.Fatal("review wrapper tool not built")
	}
	if _, err := review.Execute(context.Background(), json.RawMessage(`{"task":"again","fork_from":"sa_prev"}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.ForkFrom != "sa_prev" {
		t.Fatalf("continuation opts = %+v, want fork_from sa_prev", got)
	}
}

func TestSubagentSkillSchemasExposeOnlyContinueFromForPersistence(t *testing.T) {
	runSkill := NewRunSkillTool(New(Options{HomeDir: t.TempDir(), DisableBuiltins: true}), nil)
	runSchema := string(runSkill.Schema())
	if !strings.Contains(runSchema, `"continue_from"`) {
		t.Fatalf("run_skill schema = %s, want continue_from", runSchema)
	}
	if strings.Contains(runSchema, "fork_from") {
		t.Fatalf("run_skill schema = %s, want no fork_from", runSchema)
	}

	tools := BuiltinSubagentTools(New(Options{HomeDir: t.TempDir()}), nil)
	for _, tl := range tools {
		schema := string(tl.Schema())
		if !strings.Contains(schema, `"continue_from"`) {
			t.Fatalf("%s schema = %s, want continue_from", tl.Name(), schema)
		}
		if strings.Contains(schema, "fork_from") {
			t.Fatalf("%s schema = %s, want no fork_from", tl.Name(), schema)
		}
	}
}

func TestBuiltinSubagentToolResolvesProfile(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir()})
	tools := BuiltinSubagentTools(store, nil, func(sk Skill) *event.Profile {
		return &event.Profile{Model: sk.Name + "-model", Effort: "max"}
	})
	var review interface {
		ResolveProfile(json.RawMessage) *event.Profile
	}
	for _, tl := range tools {
		if tl.Name() == "review" {
			review = tl.(interface {
				ResolveProfile(json.RawMessage) *event.Profile
			})
			break
		}
	}
	if review == nil {
		t.Fatal("review tool not found")
	}
	got := review.ResolveProfile(json.RawMessage(`{"task":"general"}`))
	if got == nil || got.Model != "review-model" || got.Effort != "max" {
		t.Fatalf("profile = %+v, want review-model/max", got)
	}
}

func TestInstallSkill(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	tl := NewInstallSkillTool(st, nil)

	out, err := tl.Execute(context.Background(), json.RawMessage(
		`{"name":"deploy","description":"ship it","body":"steps","runAs":"subagent","model":"deepseek-pro","effort":"max","allowedTools":["bash","read_file"]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Errorf("expected ok result, got %s", out)
	}
	var res struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result JSON: %v", err)
	}
	wantPath := filepath.Join(home, ".reasonix", "skills", "deploy", SkillFile)
	if res.Path != wantPath {
		t.Fatalf("install_skill should report canonical path %s, got %s", wantPath, res.Path)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("install_skill should write canonical SKILL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".reasonix", "skills", "deploy.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install_skill should not write legacy flat deploy.md, stat err=%v", err)
	}
	// Round-trips through the store with the frontmatter we wrote.
	sk, ok := st.Read("deploy")
	if !ok {
		t.Fatal("installed skill not readable")
	}
	if sk.RunAs != RunSubagent || sk.Model != "deepseek-pro" || sk.Effort != "max" || len(sk.AllowedTools) != 2 {
		t.Errorf("frontmatter not round-tripped: runAs=%s model=%q effort=%q tools=%v", sk.RunAs, sk.Model, sk.Effort, sk.AllowedTools)
	}
	// Refuses overwrite.
	if _, err := tl.Execute(context.Background(), json.RawMessage(
		`{"name":"deploy","description":"again","body":"x"}`)); err == nil {
		t.Error("install_skill should refuse to overwrite")
	}
	// Requires description.
	if _, err := tl.Execute(context.Background(), json.RawMessage(
		`{"name":"x","description":"","body":"b"}`)); err == nil {
		t.Error("install_skill should require a description")
	}
}

func TestRenderSkillFileEmitsColorAndInvocationWhenSet(t *testing.T) {
	content := RenderSkillFile(SkillFileOptions{
		Name:        "my-agent",
		Description: "a private helper",
		Body:        "be helpful",
		RunAs:       RunSubagent,
		Color:       "amber",
		Invocation:  "manual",
	})
	for _, want := range []string{"color: amber\n", "invocation: manual\n", "runAs: subagent\n"} {
		if !strings.Contains(content, want) {
			t.Errorf("rendered content missing %q:\n%s", want, content)
		}
	}

	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	if _, err := st.CreateWithContent("my-agent", ScopeGlobal, content); err != nil {
		t.Fatalf("CreateWithContent: %v", err)
	}
	sk, ok := st.Read("my-agent")
	if !ok {
		t.Fatal("skill not readable after CreateWithContent")
	}
	if sk.Color != "amber" || sk.Invocation != "manual" {
		t.Errorf("round-trip mismatch: color=%q invocation=%q", sk.Color, sk.Invocation)
	}
}

// TestRenderSkillFileEscapesYAMLMetacharacters pins the security contract the
// reviewer flagged: free text with YAML metacharacters must round-trip intact.
// Before the yaml.v3 renderer, a description like "Review code: focus on
// security" produced an unparseable block; frontmatter.Split then returned an
// EMPTY map and the loader silently fell back to runAs=inline +
// invocation=auto — dissolving both the isolation boundary and the
// no-autodiscovery guarantee.
func TestRenderSkillFileEscapesYAMLMetacharacters(t *testing.T) {
	cases := []struct {
		label string
		desc  string
	}{
		{"colon", "Review code: focus on security"},
		{"hash", "Reviews #security and #perf tags"},
		{"double-quote", `Says "hello" politely`},
		{"single-quote", "Don't break on apostrophes"},
		{"newline", "First line\nsecond line"},
		{"leading-special", "- starts like a list item"},
		{"yaml-lookalike", "runAs: inline"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			home := t.TempDir()
			st := New(Options{HomeDir: home, DisableBuiltins: true})
			content := RenderSkillFile(SkillFileOptions{
				Name:        "esc",
				Description: tc.desc,
				Body:        "the body",
				RunAs:       RunSubagent,
				Invocation:  "manual",
			})
			if _, err := st.CreateWithContent("esc", ScopeGlobal, content); err != nil {
				t.Fatalf("CreateWithContent: %v", err)
			}
			sk, ok := st.Read("esc")
			if !ok {
				t.Fatalf("skill unreadable; rendered content:\n%s", content)
			}
			// The load-bearing assertions: the security-relevant fields must
			// survive, never silently reset to their permissive defaults.
			if sk.RunAs != RunSubagent {
				t.Errorf("RunAs = %q, want subagent (isolation lost); content:\n%s", sk.RunAs, content)
			}
			if sk.Invocation != "manual" {
				t.Errorf("Invocation = %q, want manual (autodiscovery re-enabled); content:\n%s", sk.Invocation, content)
			}
			wantDesc := strings.TrimSpace(tc.desc)
			if tc.label == "newline" {
				// frontmatter.Split returns the scalar as parsed; the multi-line
				// value survives YAML round-trip intact.
				wantDesc = "First line\nsecond line"
			}
			if sk.Description != wantDesc {
				t.Errorf("Description = %q, want %q", sk.Description, wantDesc)
			}
			if sk.Body != "the body" {
				t.Errorf("Body = %q, want %q", sk.Body, "the body")
			}
		})
	}
}

func TestRenderSkillFileOmitsColorAndInvocationByDefault(t *testing.T) {
	content := RenderSkillFile(SkillFileOptions{
		Name:        "plain-inline",
		Description: "no extras",
		Body:        "body text",
		RunAs:       RunInline,
	})
	for _, unwanted := range []string{"color:", "invocation:", "runAs:"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("rendered content should omit %q when unset:\n%s", unwanted, content)
		}
	}
}

func TestReadSkillLoadsInlineAndIsReadOnly(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/note.md", "---\ndescription: take a note\n---\nDo the thing.")
	tl := NewReadSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}))

	if !tl.ReadOnly() {
		t.Fatal("read_skill must be ReadOnly so it works in plan mode")
	}
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"note","arguments":"with args"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Do the thing.") || !strings.Contains(out, "Arguments: with args") {
		t.Errorf("inline body/args missing:\n%s", out)
	}
}

func TestReadSkillRejectsSubagent(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	tl := NewReadSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}))

	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig"}`)); err == nil || !strings.Contains(err.Error(), "run_skill") {
		t.Fatalf("read_skill on a subagent skill should point to run_skill, got %v", err)
	}
}
