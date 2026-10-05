package skill

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reasonix/internal/contract/config"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func flagStore(t *testing.T) *Store {
	t.Helper()
	home := testenv.TempDir(t)
	writeSkill(t, home, ".claude/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: ship it to prod\ndisable-model-invocation: true\n---\nDEPLOY BODY")
	writeSkill(t, home, ".claude/skills/legacyctx/SKILL.md", "---\nname: legacyctx\ndescription: legacy context\nuser-invocable: false\nargument-hint: \"[env]\"\n---\nCTX BODY")
	writeSkill(t, home, ".claude/skills/plain/SKILL.md", "---\nname: plain\ndescription: ordinary\n---\nPLAIN BODY")
	return New(Options{HomeDir: home, DisableBuiltins: true})
}

func TestFrontmatterInvocationFlagsParse(t *testing.T) {
	store := flagStore(t)
	deploy, _ := store.Read("deploy")
	if !deploy.DisableModelInvocation || deploy.DisableUserInvocation {
		t.Fatalf("deploy flags = %+v", deploy)
	}
	bg, _ := store.Read("legacyctx")
	if bg.DisableModelInvocation || !bg.DisableUserInvocation || bg.ArgumentHint != "[env]" {
		t.Fatalf("legacyctx flags = %+v", bg)
	}
	plain, _ := store.Read("plain")
	if plain.DisableModelInvocation || plain.DisableUserInvocation || plain.ArgumentHint != "" {
		t.Fatalf("plain flags = %+v", plain)
	}
}

func TestDisableModelInvocationLeavesModelListing(t *testing.T) {
	store := flagStore(t)
	index := IndexBlock(store.List())
	if strings.Contains(index, "deploy") {
		t.Fatalf("disable-model-invocation skill leaked into the model listing:\n%s", index)
	}
	if !strings.Contains(index, "legacyctx") || !strings.Contains(index, "plain") {
		t.Fatalf("model-invocable skills missing:\n%s", index)
	}
	if got := ModelInvocable(store.List()); len(got) != 2 {
		t.Fatalf("ModelInvocable = %d skills, want 2", len(got))
	}
}

func TestModelToolsRefuseDisabledSkillWithTypedError(t *testing.T) {
	store := flagStore(t)
	for name, tl := range map[string]interface {
		Execute(context.Context, json.RawMessage) (string, error)
	}{
		"run_skill":  NewRunSkillTool(store, nil),
		"read_skill": NewReadSkillTool(store),
	} {
		_, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"deploy"}`))
		if !errors.Is(err, ErrModelInvocationDisabled) {
			t.Fatalf("%s error = %v, want ErrModelInvocationDisabled", name, err)
		}
	}
	out, err := NewRunSkillTool(store, nil).Execute(context.Background(), json.RawMessage(`{"name":"legacyctx"}`))
	if err != nil || !strings.Contains(out, "CTX BODY") {
		t.Fatalf("model-only skill must stay invocable: %q %v", out, err)
	}
}

func TestDisableUserInvocationLeavesSlashSurface(t *testing.T) {
	store := flagStore(t)
	var names []string
	for _, sk := range store.SlashList() {
		names = append(names, sk.SlashName())
	}
	if strings.Join(names, ",") != "deploy,plain" {
		t.Fatalf("SlashList = %v, want deploy and plain only", names)
	}
	if _, ok := store.ReadSlash("legacyctx"); ok {
		t.Fatal("/legacyctx must not resolve when user-invocable is false")
	}
	if _, ok := store.ReadSlash("deploy"); !ok {
		t.Fatal("/deploy must stay reachable by the user")
	}
	if _, ok := store.Read("legacyctx"); !ok {
		t.Fatal("the model-side registry must still hold legacyctx")
	}
}

func TestBuiltinSubagentToolRefusesUserOnlyOverride(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".reasonix/skills/explore.md", "---\ndescription: my explore\nrunAs: subagent\ndisable-model-invocation: true\n---\nOVERRIDE BODY")
	store := New(Options{HomeDir: home, DisableBuiltins: true})
	var explore interface {
		Execute(context.Context, json.RawMessage) (string, error)
	}
	for _, tl := range BuiltinSubagentTools(store, func(context.Context, Skill, string, SubagentRunOptions) (string, error) { return "ran", nil }) {
		if tl.Name() == "explore" {
			explore = tl
		}
	}
	if explore == nil {
		t.Fatal("explore tool not registered")
	}
	if _, err := explore.Execute(context.Background(), json.RawMessage(`{"task":"look"}`)); !errors.Is(err, ErrModelInvocationDisabled) {
		t.Fatalf("explore error = %v, want ErrModelInvocationDisabled", err)
	}
}

func TestUnknownNameErrorDoesNotListUserOnlySkills(t *testing.T) {
	store := flagStore(t)
	for name, tl := range map[string]interface {
		Execute(context.Context, json.RawMessage) (string, error)
	}{
		"run_skill":       NewRunSkillTool(store, nil),
		"read_skill":      NewReadSkillTool(store),
		"read_only_skill": NewReadOnlySkillTool(store, nil),
	} {
		_, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"nope"}`))
		if err == nil || strings.Contains(err.Error(), "deploy") || !strings.Contains(err.Error(), "plain") {
			t.Fatalf("%s unknown-name error = %v; must list ordinary skills and never a user-only one", name, err)
		}
	}
}

// A hidden higher-priority skill still occupies its slash name: the shadowed
// lower-priority skill must not surface in its place.
func TestUserHiddenSkillStillShadowsSameNamedLowerLayer(t *testing.T) {
	home, project := testenv.TempDir(t), testenv.TempDir(t)
	writeSkill(t, project, ".reasonix/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: project\nuser-invocable: false\n---\nPROJECT")
	writeSkill(t, home, ".reasonix/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: user layer\n---\nUSER")
	store := New(Options{HomeDir: home, ProjectRoot: project, DisableBuiltins: true})
	if sk, ok := store.Read("deploy"); !ok || sk.Description != "project" {
		t.Fatalf("precondition: the project layer must win, got %+v", sk)
	}
	if got := store.SlashList(); len(got) != 0 {
		t.Fatalf("SlashList = %v, want none", got)
	}
	if sk, ok := store.ReadSlash("deploy"); ok {
		t.Fatalf("/deploy resolved %q", sk.Description)
	}
}

func TestInvalidYAMLSiblingDoesNotDropTheRestriction(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".reasonix/skills/ship.md", "---\nname: ship\ndescription: deploys\ndisable-model-invocation: true\nargument-hint: [a] [b]\n---\nBODY")
	sk, ok := New(Options{HomeDir: home, DisableBuiltins: true}).Read("ship")
	if !ok || !sk.DisableModelInvocation {
		t.Fatalf("a malformed sibling line freed the skill: %+v ok=%v", sk, ok)
	}
	if sk.Description != "deploys" || sk.ArgumentHint != "[a] [b]" {
		t.Fatalf("line fallback lost fields: %+v", sk)
	}
	if len(sk.Invalid) == 0 {
		t.Fatal("unparseable frontmatter was not reported")
	}
}

func TestInvalidBooleanFailsClosedAndIsReported(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".reasonix/skills/typo.md", "---\nname: typo\ndescription: d\ndisable-model-invocation: ture\nuser-invocable: flase\n---\nBODY")
	sk, _ := New(Options{HomeDir: home, DisableBuiltins: true}).Read("typo")
	if !sk.DisableModelInvocation {
		t.Fatal("an unreadable disable-model-invocation must restrict the skill")
	}
	if sk.DisableUserInvocation {
		t.Fatal("an unreadable user-invocable must not hide the skill from its user")
	}
	if len(sk.Invalid) != 2 {
		t.Fatalf("Invalid = %v, want both values reported", sk.Invalid)
	}
}

func layeredDeployStore(t *testing.T, pluginFlags, projectFlags string) *Store {
	t.Helper()
	home, project := testenv.TempDir(t), testenv.TempDir(t)
	pluginRoot := filepath.Join(testenv.TempDir(t), "skills")
	writeSkill(t, pluginRoot, "deploy/SKILL.md", "---\ndescription: plugin deploy\n"+pluginFlags+"---\nPLUGIN BODY")
	writeSkill(t, project, ".reasonix/skills/deploy/SKILL.md", "---\ndescription: project deploy\n"+projectFlags+"---\nPROJECT BODY")
	return New(Options{
		HomeDir: home, ProjectRoot: project, DisableBuiltins: true, CustomPaths: []string{pluginRoot},
		PluginPaths: map[string][]string{config.CanonicalSkillPath(pluginRoot): {"p"}},
	})
}

// The entry is registered under its qualified slash name, so the call-time
// re-read must resolve that name and not the bare one another layer also owns.
func TestModelGateResolvesTheQualifiedSlashName(t *testing.T) {
	store := layeredDeployStore(t, "", "")
	sk, err := store.ForModel("p:deploy")
	if err != nil || !strings.Contains(sk.Body, "PLUGIN BODY") {
		t.Fatalf("ForModel(p:deploy) = %q, %v; want the plugin skill", sk.Body, err)
	}
	if sk, err := store.ForModel("deploy"); err != nil || !strings.Contains(sk.Body, "PROJECT BODY") {
		t.Fatalf("ForModel(deploy) = %q, %v; want the project skill", sk.Body, err)
	}

	store = layeredDeployStore(t, "", "disable-model-invocation: true\n")
	if _, err := store.ForModel("p:deploy"); err != nil {
		t.Fatalf("a restricted project skill must not refuse the plugin skill: %v", err)
	}
	if _, err := store.ForModel("deploy"); !errors.Is(err, ErrModelInvocationDisabled) {
		t.Fatalf("restricted project skill: %v", err)
	}

	store = layeredDeployStore(t, "disable-model-invocation: true\n", "")
	if _, err := store.ForModel("p:deploy"); !errors.Is(err, ErrModelInvocationDisabled) {
		t.Fatalf("restricted plugin skill: %v", err)
	}
	if _, err := store.ForModel("deploy"); err != nil {
		t.Fatalf("a restricted plugin skill must not refuse the project skill: %v", err)
	}
}

func TestFallbackScanKeepsAnyRestrictingDeclaration(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".reasonix/skills/dup.md", "---\nname: dup\ndescription: d\ndisable-model-invocation: true\nargument-hint: [a] [b]\ndisable-model-invocation: false\n---\nBODY")
	if sk, _ := New(Options{HomeDir: home, DisableBuiltins: true}).Read("dup"); !sk.DisableModelInvocation {
		t.Fatal("a later `false` released a restriction in the fallback scan")
	}
}
