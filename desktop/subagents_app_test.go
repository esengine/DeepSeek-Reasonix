package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/skill"
)

func newTestSubagentApp(t *testing.T) *App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	st := skill.New(skill.Options{HomeDir: home})
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{AllSkillStore: st, SkillStore: st}), "")
	t.Cleanup(func() { a.activeCtrl().Close() })
	return a
}

func TestCreateSubagentProfileWritesManualInvocationSubagentSkill(t *testing.T) {
	a := newTestSubagentApp(t)
	path, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name:         "my-formatter",
		Description:  "formats code the way I like",
		SystemPrompt: "You are a code formatting assistant.",
		Color:        "amber",
		AllowedTools: []string{"read_file", "edit_file"},
		Scope:        "global",
	})
	if err != nil {
		t.Fatalf("CreateSubagentProfile: %v", err)
	}
	if path == "" {
		t.Fatal("expected a non-empty path")
	}

	views := a.SkillsSettings().Skills
	var found *SkillView
	for i := range views {
		if views[i].Name == "my-formatter" {
			found = &views[i]
		}
	}
	if found == nil {
		t.Fatalf("created profile missing from SkillsSettings: %+v", views)
	}
	if found.RunAs != "subagent" || found.Invocation != "/my-formatter" || found.InvocationMode != "manual" || found.Color != "amber" {
		t.Fatalf("profile fields wrong: %+v", found)
	}
}

func TestCreateSubagentProfileRejectsBuiltinNameCollision(t *testing.T) {
	a := newTestSubagentApp(t)
	_, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name:         "explore",
		Description:  "shadow the built-in",
		SystemPrompt: "do something else entirely",
	})
	if err == nil {
		t.Fatal("expected an error naming a built-in subagent")
	}
}

func TestCreateSubagentProfileRejectsReservedSlashNames(t *testing.T) {
	for _, name := range []string{"clear", "mcp__server__prompt"} {
		t.Run(name, func(t *testing.T) {
			a := newTestSubagentApp(t)
			_, err := a.CreateSubagentProfile(SubagentProfileInput{Name: name, Description: "d", SystemPrompt: "body"})
			if err == nil || !strings.Contains(err.Error(), "slash command namespace") {
				t.Fatalf("CreateSubagentProfile(%q) error = %v", name, err)
			}
		})
	}
}

func TestCreateSubagentProfileRejectsCustomCommandCollision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	st := skill.New(skill.Options{HomeDir: home})
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{
		AllSkillStore: st,
		SkillStore:    st,
		Commands:      []command.Command{{Name: "formatter"}},
	}), "")
	t.Cleanup(func() { a.activeCtrl().Close() })
	_, err := a.CreateSubagentProfile(SubagentProfileInput{Name: "formatter", Description: "d", SystemPrompt: "body"})
	if err == nil || !strings.Contains(err.Error(), "slash command namespace") {
		t.Fatalf("custom command collision error = %v", err)
	}
}

func TestCreateSubagentProfileRejectsDuplicateName(t *testing.T) {
	a := newTestSubagentApp(t)
	input := SubagentProfileInput{Name: "dup", Description: "first", SystemPrompt: "body"}
	if _, err := a.CreateSubagentProfile(input); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := a.CreateSubagentProfile(input); err == nil {
		t.Fatal("expected an error creating a duplicate name")
	}
}

func TestCreateSubagentProfileRequiresDescriptionAndPrompt(t *testing.T) {
	a := newTestSubagentApp(t)
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{Name: "x", SystemPrompt: "body"}); err == nil {
		t.Error("expected an error for a missing description")
	}
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{Name: "x", Description: "d"}); err == nil {
		t.Error("expected an error for a missing system prompt")
	}
}

func TestCreateSubagentProfileScopeIsStrictButEmptyRemainsGlobal(t *testing.T) {
	a := newTestSubagentApp(t)
	path, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "default-global", Description: "d", SystemPrompt: "body",
	})
	if err != nil {
		t.Fatalf("empty scope should preserve the legacy global default: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(path), "/.reasonix/skills/") {
		t.Fatalf("empty scope path = %q, want global Reasonix skills dir", path)
	}
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "bad-scope", Description: "d", SystemPrompt: "body", Scope: "custom",
	}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("custom create scope error = %v, want explicit rejection", err)
	}
	for _, sk := range a.SkillsSettings().Skills {
		if sk.Name == "bad-scope" {
			t.Fatal("rejected custom scope must not fall back to a global file")
		}
	}
}

func TestUpdateAndDeleteSubagentProfileRejectUnsupportedScopeWithoutTouchingGlobal(t *testing.T) {
	a := newTestSubagentApp(t)
	path, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "scope-guard", Description: "original", SystemPrompt: "body", Scope: "global",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateSubagentProfile("scope-guard", "custom", SubagentProfileInput{
		Description: "changed", SystemPrompt: "changed body",
	}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("custom update scope error = %v, want explicit rejection", err)
	}
	if err := a.DeleteSubagentProfile("scope-guard", "anything-else"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown delete scope error = %v, want explicit rejection", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("rejected delete removed the global profile: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("rejected custom update modified the global profile:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestUpdateSubagentProfileOverwritesFields(t *testing.T) {
	a := newTestSubagentApp(t)
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "editable-agent", Description: "v1", SystemPrompt: "old body", Color: "amber", Scope: "global",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.UpdateSubagentProfile("editable-agent", "global", SubagentProfileInput{
		Description: "v2", SystemPrompt: "new body", Color: "blue", Model: "deepseek/deepseek-pro", AllowedTools: []string{"read_file"},
	}); err != nil {
		t.Fatalf("UpdateSubagentProfile: %v", err)
	}
	var found *SkillView
	for _, sk := range a.SkillsSettings().Skills {
		if sk.Name == "editable-agent" {
			found = &sk
		}
	}
	if found == nil {
		t.Fatal("editable-agent missing after update")
	}
	if found.Description != "v2" || found.Color != "blue" || found.Model != "deepseek/deepseek-pro" || found.Invocation != "/editable-agent" || found.InvocationMode != "manual" || found.RunAs != "subagent" {
		t.Fatalf("update did not apply as expected: %+v", found)
	}
	if len(found.AllowedTools) != 1 || found.AllowedTools[0] != "read_file" {
		t.Fatalf("AllowedTools not updated: %v", found.AllowedTools)
	}
}

func TestUpdateSubagentProfileRequiresDescriptionAndPrompt(t *testing.T) {
	a := newTestSubagentApp(t)
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "editable-agent2", Description: "v1", SystemPrompt: "old body", Scope: "global",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.UpdateSubagentProfile("editable-agent2", "global", SubagentProfileInput{SystemPrompt: "new body"}); err == nil {
		t.Error("expected an error for a missing description")
	}
	if err := a.UpdateSubagentProfile("editable-agent2", "global", SubagentProfileInput{Description: "d"}); err == nil {
		t.Error("expected an error for a missing system prompt")
	}
}

func TestUpdateSubagentProfileRefusesNonManualSkill(t *testing.T) {
	a := newTestSubagentApp(t)
	home := os.Getenv("HOME")
	// A hand-authored subagent skill without invocation: manual — the exact
	// shape the reviewer flagged: editing it here would silently drop fields.
	dir := filepath.Join(home, ".reasonix", "skills", "hand-authored")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\ndescription: hand written\nrunAs: subagent\nread-only: true\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := a.UpdateSubagentProfile("hand-authored", "global", SubagentProfileInput{Description: "x", SystemPrompt: "y"})
	if err == nil {
		t.Fatal("expected refusal for a non-manual skill")
	}
	if !strings.Contains(err.Error(), "manual") {
		t.Fatalf("error should explain the manual-invocation rule, got: %v", err)
	}
}

func TestUpdateSubagentProfileRefusesUnmanagedFrontmatter(t *testing.T) {
	a := newTestSubagentApp(t)
	home := os.Getenv("HOME")
	// invocation: manual but carrying an unmanaged routing key — dropping it
	// on save would silently change discovery/auto-use semantics.
	dir := filepath.Join(home, ".reasonix", "skills", "manual-rich")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\ndescription: locked down\nrunAs: subagent\ninvocation: manual\ntriggers: [deploy]\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := a.UpdateSubagentProfile("manual-rich", "global", SubagentProfileInput{Description: "x", SystemPrompt: "y"})
	if err == nil {
		t.Fatal("expected refusal for unmanaged frontmatter keys")
	}
	if !strings.Contains(err.Error(), "triggers") {
		t.Fatalf("error should name the unmanaged key, got: %v", err)
	}
	// The file must be untouched by the refused edit.
	raw, rerr := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if rerr != nil || !strings.Contains(string(raw), "triggers:") {
		t.Fatalf("refused edit must not modify the file, got: %s (%v)", raw, rerr)
	}
}

func TestUpdateSubagentProfileRoundTripsReadOnly(t *testing.T) {
	a := newTestSubagentApp(t)
	path, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "ro-agent", Description: "readonly", SystemPrompt: "stay read only",
		ReadOnly: true, Scope: "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "read-only: true") {
		t.Fatalf("create should emit read-only frontmatter, got:\n%s", raw)
	}
	if err := a.UpdateSubagentProfile("ro-agent", "global", SubagentProfileInput{
		Description: "readonly v2", SystemPrompt: "still read only", ReadOnly: true,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "read-only: true") {
		t.Fatalf("update must preserve read-only, got:\n%s", raw)
	}
	if !strings.Contains(string(raw), "readonly v2") {
		t.Fatalf("update must change description, got:\n%s", raw)
	}
}

// TestUpdateSubagentProfileRefusesManualInlineSkill pins the runAs guard: a
// hand-authored manual-invocation INLINE skill carries only editor-managed
// frontmatter keys, so without an explicit runAs check the update path would
// rewrite it with runAs: subagent — silently converting an inline playbook
// into an isolated subagent.
func TestUpdateSubagentProfileRefusesManualInlineSkill(t *testing.T) {
	a := newTestSubagentApp(t)
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".reasonix", "skills", "manual-inline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\ndescription: quiet inline playbook\ninvocation: manual\n---\ninline body"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := a.UpdateSubagentProfile("manual-inline", "global", SubagentProfileInput{Description: "x", SystemPrompt: "y"})
	if err == nil {
		t.Fatal("expected refusal for a manual inline skill")
	}
	if !strings.Contains(err.Error(), "subagent") {
		t.Fatalf("error should explain the runAs rule, got: %v", err)
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if rerr != nil || strings.Contains(string(raw), "runAs: subagent") {
		t.Fatalf("refused edit must not convert the inline skill, got: %s (%v)", raw, rerr)
	}
}

// TestDeleteSubagentProfileRefusesNonProfileSkill pins the delete guard: the
// bridge method must not remove a user skill this page never owned, even when
// called directly with a matching name+scope.
func TestDeleteSubagentProfileRefusesNonProfileSkill(t *testing.T) {
	a := newTestSubagentApp(t)
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".reasonix", "skills", "hand-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(file,
		[]byte("---\ndescription: precious hand-authored playbook\nrunAs: subagent\ntriggers: deploy\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSubagentProfile("hand-skill", "global"); err == nil {
		t.Fatal("expected refusal deleting a non-profile skill")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("refused delete must leave the file in place: %v", err)
	}
}

func TestUpdateSubagentProfileRefusesExpandedReferences(t *testing.T) {
	a := newTestSubagentApp(t)
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".reasonix", "skills", "with-refs")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\ndescription: has refs\nrunAs: subagent\ninvocation: manual\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "extra.md"), []byte("depth material"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := a.UpdateSubagentProfile("with-refs", "global", SubagentProfileInput{Description: "x", SystemPrompt: "y"})
	if err == nil {
		t.Fatal("expected refusal for a profile with a references/ dir")
	}
	if !strings.Contains(err.Error(), "references") {
		t.Fatalf("error should name the references dir, got: %v", err)
	}
}

func TestUpdateSubagentProfileWrongScopeFailsSafely(t *testing.T) {
	a := newTestSubagentApp(t)
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "editable-agent3", Description: "v1", SystemPrompt: "old body", Scope: "global",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.UpdateSubagentProfile("editable-agent3", "project", SubagentProfileInput{Description: "v2", SystemPrompt: "new body"}); err == nil {
		t.Fatal("expected an error updating with the wrong scope")
	}
	for _, sk := range a.SkillsSettings().Skills {
		if sk.Name == "editable-agent3" && sk.Description != "v1" {
			t.Fatalf("profile should be unchanged after a refused scope-mismatched update, got description=%q", sk.Description)
		}
	}
}

func TestTrySubagentRegistryIsReadOnly(t *testing.T) {
	reg := trySubagentToolRegistry(config.Default(), t.TempDir(), nil)
	for _, writer := range []string{"write_file", "edit_file", "multi_edit", "move_file", "notebook_edit", "delete_range", "delete_symbol"} {
		if _, ok := reg.Get(writer); ok {
			t.Errorf("try registry should strip writer tool %q; got %v", writer, reg.Names())
		}
	}
	for _, meta := range []string{"task", "run_skill", "install_skill", "install_source", "parallel_tasks", "fleet"} {
		if _, ok := reg.Get(meta); ok {
			t.Errorf("try registry should strip meta/delegation tool %q; got %v", meta, reg.Names())
		}
	}
	if _, ok := reg.Get("read_file"); !ok {
		t.Fatalf("try registry should keep read_file; got %v", reg.Names())
	}
}

func TestTrySubagentRegistryBashEnforcesReadOnlyPolicy(t *testing.T) {
	reg := trySubagentToolRegistry(config.Default(), t.TempDir(), nil)
	bash, ok := reg.Get("bash")
	if !ok {
		t.Fatalf("try registry should keep bash; got %v", reg.Names())
	}
	if !bash.ReadOnly() {
		t.Fatal("try bash should report ReadOnly=true (restricted read-only wrapper)")
	}
	out, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"rm -rf /tmp/x"}`))
	if err != nil {
		t.Fatalf("blocked command should return a message, not an error: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "plan mode") && !strings.Contains(strings.ToLower(out), "blocked") && !strings.Contains(strings.ToLower(out), "not allowed") {
		t.Fatalf("write-capable command should be blocked by the read-only policy, got: %s", out)
	}
}

func TestTrySubagentRegistryHonorsAllowedTools(t *testing.T) {
	reg := trySubagentToolRegistry(config.Default(), t.TempDir(), []string{"read_file", "grep", "write_file"})
	if _, ok := reg.Get("read_file"); !ok {
		t.Fatalf("allowlisted read_file missing; got %v", reg.Names())
	}
	if _, ok := reg.Get("write_file"); ok {
		t.Fatalf("write_file must stay stripped even when allowlisted; got %v", reg.Names())
	}
	if _, ok := reg.Get("ls"); ok {
		t.Fatalf("ls not in the allowlist, should be absent; got %v", reg.Names())
	}
}

// TestTrySubagentRegistryResolvesRelativePathsAgainstWorkspaceRoot pins the
// multi-workspace contract: the try registry's tools must resolve relative
// paths against the ACTIVE TAB's root, not the desktop process CWD. The
// process working directory is global and, in a multi-tab session, points at
// whichever project the app happened to start in — a try run resolving
// against it could read (and send to the provider) a different project than
// the one on screen.
func TestTrySubagentRegistryResolvesRelativePathsAgainstWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("workspace-bound"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if cwd == root {
		t.Fatal("test requires process CWD != workspace root")
	}

	reg := trySubagentToolRegistry(config.Default(), root, nil)
	rf, ok := reg.Get("read_file")
	if !ok {
		t.Fatalf("read_file missing; got %v", reg.Names())
	}
	out, err := rf.Execute(context.Background(), json.RawMessage(`{"path":"marker.txt"}`))
	if err != nil {
		t.Fatalf("relative read against the workspace root failed (resolved against process CWD?): %v", err)
	}
	if !strings.Contains(out, "workspace-bound") {
		t.Fatalf("relative read returned wrong content: %s", out)
	}

	ls, ok := reg.Get("ls")
	if !ok {
		t.Fatalf("ls missing; got %v", reg.Names())
	}
	out, err = ls.Execute(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("relative ls against the workspace root failed: %v", err)
	}
	if !strings.Contains(out, "marker.txt") {
		t.Fatalf("ls of workspace root missing marker.txt (listed process CWD instead?): %s", out)
	}
}

func TestTrySubagentProfileRequiresTaskAndPrompt(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	if _, err := a.TrySubagentProfile(SubagentProfileInput{SystemPrompt: "be helpful"}, ""); err == nil {
		t.Error("expected an error for a missing task")
	}
	if _, err := a.TrySubagentProfile(SubagentProfileInput{}, "do something"); err == nil {
		t.Error("expected an error for a missing system prompt")
	}
}

func TestTrySubagentProfileRejectsUnknownModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	_, err := a.TrySubagentProfile(SubagentProfileInput{
		SystemPrompt: "be helpful",
		Model:        "nope/does-not-exist",
	}, "do something")
	if err == nil {
		t.Error("expected an error for an unresolvable model ref")
	}
}

func TestDeleteSubagentProfileRemovesIt(t *testing.T) {
	a := newTestSubagentApp(t)
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "temp-agent", Description: "d", SystemPrompt: "body", Scope: "global",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.DeleteSubagentProfile("temp-agent", "global"); err != nil {
		t.Fatalf("DeleteSubagentProfile: %v", err)
	}
	for _, sk := range a.SkillsSettings().Skills {
		if sk.Name == "temp-agent" {
			t.Fatal("deleted profile still present")
		}
	}
}

func TestSetSubagentProfileModelAndEffortRoundTripPerName(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	app := NewApp()
	if err := app.SetSubagentProfileModel("explore", "deepseek/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetSubagentProfileModel: %v", err)
	}
	if err := app.SetSubagentProfileEffort("explore", "max"); err != nil {
		t.Fatalf("SetSubagentProfileEffort: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.SubagentModels["explore"] != "deepseek/deepseek-v4-pro" || cfg.Agent.SubagentEfforts["explore"] != "max" {
		t.Fatalf("saved per-name overrides = model:%q effort:%q", cfg.Agent.SubagentModels["explore"], cfg.Agent.SubagentEfforts["explore"])
	}
	// A different skill name must be unaffected — this is a per-name map, not
	// a global default.
	if cfg.Agent.SubagentModel != "" || cfg.Agent.SubagentEffort != "" {
		t.Fatalf("global subagent defaults should be untouched: model:%q effort:%q", cfg.Agent.SubagentModel, cfg.Agent.SubagentEffort)
	}

	// Clearing (empty ref/level) removes the map entry rather than storing "".
	if err := app.SetSubagentProfileModel("explore", ""); err != nil {
		t.Fatalf("clear SetSubagentProfileModel: %v", err)
	}
	if err := app.SetSubagentProfileEffort("explore", ""); err != nil {
		t.Fatalf("clear SetSubagentProfileEffort: %v", err)
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	if _, ok := cfg.Agent.SubagentModels["explore"]; ok {
		t.Fatalf("cleared model override should be removed, got %+v", cfg.Agent.SubagentModels)
	}
	if _, ok := cfg.Agent.SubagentEfforts["explore"]; ok {
		t.Fatalf("cleared effort override should be removed, got %+v", cfg.Agent.SubagentEfforts)
	}
}

func TestSubagentOverrideAliasesReadAndClear(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	// A legacy underscore-key override for the security-review skill — the
	// runtime dispatch (boot.SubagentModelKeys) honors it, so the UI must
	// both display it and clear it.
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[agent.subagent_models]
security_review = "deepseek/deepseek-v4-pro"

[agent.subagent_efforts]
security_review = "max"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Read side: the alias entry must surface for the hyphenated skill name.
	cfg := config.LoadForEdit(config.UserConfigPath())
	if got := subagentOverrideFor(cfg.Agent.SubagentModels, "security-review"); got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("alias model override not visible: %q", got)
	}
	if got := subagentOverrideFor(cfg.Agent.SubagentEfforts, "security-review"); got != "max" {
		t.Fatalf("alias effort override not visible: %q", got)
	}

	// Clear side: clearing by the hyphenated name must remove the underscore
	// entry too, or the override silently stays live at dispatch time.
	app := NewApp()
	if err := app.SetSubagentProfileModel("security-review", ""); err != nil {
		t.Fatalf("clear model: %v", err)
	}
	if err := app.SetSubagentProfileEffort("security-review", ""); err != nil {
		t.Fatalf("clear effort: %v", err)
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	if v, ok := cfg.Agent.SubagentModels["security_review"]; ok {
		t.Fatalf("legacy alias model entry survived the clear: %q", v)
	}
	if v, ok := cfg.Agent.SubagentEfforts["security_review"]; ok {
		t.Fatalf("legacy alias effort entry survived the clear: %q", v)
	}
}

func TestSetSubagentProfileModelSweepsAliasOnSet(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[agent.subagent_models]
security_review = "deepseek/deepseek-v4-flash"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	app := NewApp()
	if err := app.SetSubagentProfileModel("security-review", "deepseek/deepseek-v4-pro"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if _, ok := cfg.Agent.SubagentModels["security_review"]; ok {
		t.Fatalf("stale alias entry should be swept on set: %+v", cfg.Agent.SubagentModels)
	}
	if got := cfg.Agent.SubagentModels["security-review"]; got != "deepseek/deepseek-v4-pro" {
		t.Fatalf("canonical entry = %q, want deepseek/deepseek-v4-pro", got)
	}
}

func TestSetSubagentProfileModelRejectsUnknownModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if err := app.SetSubagentProfileModel("explore", "nope/does-not-exist"); err == nil {
		t.Error("expected an error for an unresolvable model ref")
	}
}

func TestSkillsSettingsSurfacesConfiguredModelOverride(t *testing.T) {
	a := newTestSubagentApp(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[agent.subagent_models]
explore = "deepseek/deepseek-v4-pro"

[agent.subagent_efforts]
explore = "max"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	found := false
	for _, sk := range a.SkillsSettings().Skills {
		if sk.Name != "explore" {
			continue
		}
		found = true
		if sk.ConfiguredModel != "deepseek/deepseek-v4-pro" || sk.ConfiguredEffort != "max" {
			t.Fatalf("explore configured override = model:%q effort:%q", sk.ConfiguredModel, sk.ConfiguredEffort)
		}
	}
	if !found {
		t.Fatal("explore not present in SkillsSettings")
	}
}

func TestDeleteSubagentProfileWrongScopeFailsSafely(t *testing.T) {
	a := newTestSubagentApp(t)
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "scoped-agent", Description: "d", SystemPrompt: "body", Scope: "global",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.DeleteSubagentProfile("scoped-agent", "project"); err == nil {
		t.Fatal("expected an error deleting with the wrong scope")
	}
	found := false
	for _, sk := range a.SkillsSettings().Skills {
		if sk.Name == "scoped-agent" {
			found = true
		}
	}
	if !found {
		t.Fatal("profile should survive a refused scope-mismatched delete")
	}
}

// Profile CRUD must refuse up front while the controller has active runtime
// work: the post-save RefreshSkills rebuild would be rejected anyway, and a
// file already written (or deleted) by then strands the UI — the save reports
// failure, the list never refreshes, and a create retry hits "already
// exists". Mirrors the applyConfigChange precheck contract.
func TestSubagentProfileCRUDRefusesWhileControllerBusy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	st := skill.New(skill.Options{HomeDir: home})
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	a := NewApp()
	a.setTestCtrl(control.New(control.Options{Runner: runner, AllSkillStore: st, SkillStore: st}), "")
	ctrl := a.activeCtrl()
	defer ctrl.Close()

	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "busy-target", Description: "d", SystemPrompt: "body", Scope: "global",
	}); err != nil {
		t.Fatalf("create while idle: %v", err)
	}

	ctrl.Submit("work")
	<-runner.started

	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "busy-new", Description: "d", SystemPrompt: "body", Scope: "global",
	}); err == nil || !strings.Contains(err.Error(), "before changing subagents") {
		t.Fatalf("busy create error = %v, want the active-work guard", err)
	}
	if _, ok := st.Read("busy-new"); ok {
		t.Fatal("busy-rejected create must not write the profile file")
	}
	if err := a.UpdateSubagentProfile("busy-target", "global", SubagentProfileInput{
		Description: "changed", SystemPrompt: "changed body",
	}); err == nil || !strings.Contains(err.Error(), "before changing subagents") {
		t.Fatalf("busy update error = %v, want the active-work guard", err)
	}
	if sk, ok := st.Read("busy-target"); !ok || sk.Description != "d" {
		t.Fatalf("busy-rejected update must leave the file unchanged, got %+v ok=%v", sk, ok)
	}
	if err := a.DeleteSubagentProfile("busy-target", "global"); err == nil || !strings.Contains(err.Error(), "before changing subagents") {
		t.Fatalf("busy delete error = %v, want the active-work guard", err)
	}
	if _, ok := st.Read("busy-target"); !ok {
		t.Fatal("busy-rejected delete must keep the profile file")
	}

	close(runner.release)
	waitNotRunning(t, ctrl)

	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "busy-new", Description: "d", SystemPrompt: "body", Scope: "global",
	}); err != nil {
		t.Fatalf("create after the turn settled: %v", err)
	}
}

// A try run must be cancellable (it is otherwise an unstoppable 12-step
// provider loop) and single-flight: a second concurrent try is refused
// instead of racing the first one's cancel handle.
func TestTrySubagentProfileCancelAbortsRunAndIsSingleFlight(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

	requestStarted := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()

	cfg := config.Default()
	cfg.DefaultModel = "prov-t/model-t1"
	cfg.Providers = []config.ProviderEntry{
		{Name: "prov-t", Kind: "openai", BaseURL: srv.URL, Model: "model-t1", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	a := NewApp()
	done := make(chan error, 1)
	go func() {
		_, err := a.TrySubagentProfile(SubagentProfileInput{SystemPrompt: "be helpful"}, "do something")
		done <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("try run never reached the provider")
	}
	if _, err := a.TrySubagentProfile(SubagentProfileInput{SystemPrompt: "p"}, "task"); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("concurrent try error = %v, want the single-flight refusal", err)
	}

	a.CancelTrySubagentProfile()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled try run should return an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled try run did not return")
	}

	// The slot frees up after the run settles: a fresh cancel is a no-op and
	// a new try is admitted. Release the handler first so this run fails
	// fast on the invalid empty response instead of blocking on the hang.
	releaseAll()
	a.CancelTrySubagentProfile()
	if _, err := a.TrySubagentProfile(SubagentProfileInput{SystemPrompt: "p"}, "task"); err != nil && strings.Contains(err.Error(), "in progress") {
		t.Fatalf("slot did not free after cancel: %v", err)
	}
}

// SkillView.Body is the Subagents editor's prompt prefill and must ship only
// for runAs=subagent skills — inline skills fold references/ into Body at
// load time and would bloat every Capabilities/Settings fetch.
func TestSkillsSettingsBodyOnlyForSubagentSkills(t *testing.T) {
	a := newTestSubagentApp(t)
	if _, err := a.CreateSubagentProfile(SubagentProfileInput{
		Name: "body-agent", Description: "d", SystemPrompt: "subagent prompt body", Scope: "global",
	}); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := a.activeCtrl().CreateSkill("plain-notes", skill.ScopeGlobal,
		"---\nname: plain-notes\ndescription: notes\n---\n\nbig inline body\n"); err != nil {
		t.Fatalf("create inline skill: %v", err)
	}
	var sawProfile, sawInline bool
	for _, view := range a.SkillsSettings().Skills {
		switch view.Name {
		case "body-agent":
			sawProfile = true
			if view.Body != "subagent prompt body" {
				t.Fatalf("subagent profile Body = %q, want the prompt", view.Body)
			}
		case "plain-notes":
			sawInline = true
			if view.Body != "" {
				t.Fatalf("inline skill Body should be omitted, got %q", view.Body)
			}
		}
	}
	if !sawProfile || !sawInline {
		t.Fatalf("views missing: profile=%v inline=%v", sawProfile, sawInline)
	}
}
