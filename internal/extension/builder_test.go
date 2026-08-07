package extension

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// staticContributor returns a contributor with a fixed name and contribution
// list — the test stand-in for a real discovery adapter.
func staticContributor(name string, contribs ...Contribution) Contributor {
	return ContributorFunc{
		ContributorName: name,
		Fn:              func(context.Context) ([]Contribution, error) { return contribs, nil },
	}
}

// determinismContributors builds a mixed set of contributors: cross-tier
// tool shadowing, skills, commands, additive hooks, and interceptors with
// overlapping priorities and plugin IDs.
func determinismContributors() []Contributor {
	return []Contributor{
		staticContributor("tools-builtin",
			Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopeBuiltin, "", "builtin"), Payload: schemaPayload("read_file", "builtin read")},
			Contribution{Kind: KindTool, ID: "write_file", Source: src(ScopeBuiltin, "", "builtin"), Payload: schemaPayload("write_file", "builtin write")},
		),
		staticContributor("tools-project",
			// Project tier shadows the builtin read_file.
			Contribution{Kind: KindTool, ID: "read_file", Source: src(ScopeProject, "", "project"), Payload: schemaPayload("read_file", "project read")},
			Contribution{Kind: KindTool, ID: "grep", Source: src(ScopeProject, "", "project"), Payload: schemaPayload("grep", "project grep")},
		),
		staticContributor("skills",
			Contribution{Kind: KindSkill, ID: "review", Source: src(ScopeProject, "", "project"), Payload: "review body"},
			Contribution{Kind: KindSkill, ID: "lint", Source: src(ScopeGlobal, "", "user"), Payload: "lint body"},
		),
		staticContributor("commands",
			Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopeProject, "", "project"), Payload: "deploy body"},
		),
		staticContributor("hooks",
			Contribution{Kind: KindHook, ID: "PreToolUse#0", Source: src(ScopeProject, "", "project"), Payload: "hook-a"},
			Contribution{Kind: KindHook, ID: "PreToolUse#1", Source: src(ScopeGlobal, "", "global"), Payload: "hook-b"},
		),
		staticContributor("interceptors-a",
			Contribution{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: 10, Source: src(ScopePlugin, "plug-b", "plugin"), Payload: "i1"},
			Contribution{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: -5, Source: src(ScopePlugin, "plug-a", "plugin"), Payload: "i2"},
			Contribution{Kind: KindInterceptor, ID: string(PointProviderRequest), Priority: 0, Source: src(ScopeProject, "", "project"), Payload: "i3"},
		),
		staticContributor("interceptors-b",
			// Same priority and plugin as one above: per-contributor order
			// breaks the tie, so the chain must stay stable across
			// contributor permutations.
			Contribution{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: -5, Source: src(ScopePlugin, "plug-a", "plugin"), Payload: "i4"},
		),
	}
}

// TestBuildDeterminism permutes contributor registration order 100 times and
// requires byte-identical snapshots. Registration order is caller-controlled
// and arbitrary; the snapshot may only depend on contribution data.
func TestBuildDeterminism(t *testing.T) {
	contributors := determinismContributors()
	type fingerprint struct {
		schemas []byte
		chains  []byte
		catalog []byte
		hash    string
	}
	var reference *fingerprint
	for seed := range int64(100) {
		r := rand.New(rand.NewSource(seed))
		perm := r.Perm(len(contributors))
		b := NewBuilder().WithSystemPrompt("system prompt v1").WithGeneration(7)
		for _, idx := range perm {
			b.AddContributor(contributors[idx])
		}
		snap, _, err := b.Build(context.Background())
		if err != nil {
			t.Fatalf("seed %d: Build failed: %v", seed, err)
		}
		schemasJSON, err := json.Marshal(snap.ToolSchemas())
		if err != nil {
			t.Fatalf("seed %d: marshal schemas: %v", seed, err)
		}
		chainsJSON, err := json.Marshal(snap.InterceptorChain())
		if err != nil {
			t.Fatalf("seed %d: marshal chains: %v", seed, err)
		}
		catalogJSON, err := json.Marshal(snap.Catalog().All())
		if err != nil {
			t.Fatalf("seed %d: marshal catalog: %v", seed, err)
		}
		got := fingerprint{schemas: schemasJSON, chains: chainsJSON, catalog: catalogJSON, hash: snap.CacheHash()}
		if reference == nil {
			reference = &got
			continue
		}
		if string(got.schemas) != string(reference.schemas) {
			t.Fatalf("seed %d: ToolSchemas order diverged:\n%s\nvs\n%s", seed, got.schemas, reference.schemas)
		}
		if string(got.chains) != string(reference.chains) {
			t.Fatalf("seed %d: InterceptorChain order diverged:\n%s\nvs\n%s", seed, got.chains, reference.chains)
		}
		if string(got.catalog) != string(reference.catalog) {
			t.Fatalf("seed %d: catalog order diverged", seed)
		}
		if got.hash != reference.hash {
			t.Fatalf("seed %d: CacheHash diverged: %s vs %s", seed, got.hash, reference.hash)
		}
	}
	// The cross-tier shadow must resolve to the project tool regardless of
	// ordering — check the reference fingerprint content, not just equality.
	b := NewBuilder().WithSystemPrompt("system prompt v1")
	b.AddContributor(contributors...)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("reference build: %v", err)
	}
	for _, s := range snap.ToolSchemas() {
		if s.Name == "read_file" && s.Description != "project read" {
			t.Fatalf("read_file winner = %q, want project-tier schema", s.Description)
		}
	}
	// Tool schemas must be sorted by name.
	names := []string{}
	for _, s := range snap.ToolSchemas() {
		names = append(names, s.Name)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("ToolSchemas not sorted: %v", names)
		}
	}
}

// TestConflictCommandSameTier: two plugins offering the same command ID is a
// hard failure naming both, not a silent last-writer-wins.
func TestConflictCommandSameTier(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(
		staticContributor("a", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pa", "plugin"), Payload: "a"}),
		staticContributor("b", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pb", "plugin"), Payload: "b"}),
	)
	_, _, err := b.Build(context.Background())
	if err == nil {
		t.Fatal("Build succeeded, want ConflictError")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error %v is not a *ConflictError", err)
	}
	if conflict.Kind != KindCommand || conflict.ID != "deploy" {
		t.Fatalf("conflict = (%s, %s), want (command, deploy)", conflict.Kind, conflict.ID)
	}
	if !strings.Contains(err.Error(), "pa") || !strings.Contains(err.Error(), "pb") {
		t.Fatalf("conflict error must name both plugins, got: %v", err)
	}
}

// TestConflictProviderSameTier pins the same rule for provider refs.
func TestConflictProviderSameTier(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(
		staticContributor("a", Contribution{Kind: KindProvider, ID: "openai/gpt-5", Source: src(ScopePlugin, "pa", "plugin"), Payload: provider.Descriptor{Ref: "openai/gpt-5"}}),
		staticContributor("b", Contribution{Kind: KindProvider, ID: "openai/gpt-5", Source: src(ScopePlugin, "pb", "plugin"), Payload: provider.Descriptor{Ref: "openai/gpt-5"}}),
	)
	_, _, err := b.Build(context.Background())
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Build error = %v, want *ConflictError", err)
	}
	if conflict.Kind != KindProvider || conflict.ID != "openai/gpt-5" {
		t.Fatalf("conflict = (%s, %s), want (provider, openai/gpt-5)", conflict.Kind, conflict.ID)
	}
}

// TestConflictMCPServerSameTier pins the same rule for MCP server names.
func TestConflictMCPServerSameTier(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(
		staticContributor("a", Contribution{Kind: KindMCPServer, ID: "fs", Source: src(ScopePlugin, "pa", "plugin"), Payload: "spec-a"}),
		staticContributor("b", Contribution{Kind: KindMCPServer, ID: "fs", Source: src(ScopePlugin, "pb", "plugin"), Payload: "spec-b"}),
	)
	_, _, err := b.Build(context.Background())
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Build error = %v, want *ConflictError", err)
	}
	if conflict.Kind != KindMCPServer || conflict.ID != "fs" {
		t.Fatalf("conflict = (%s, %s), want (mcp_server, fs)", conflict.Kind, conflict.ID)
	}
}

// TestCrossTierShadows: the same canonical ID at different tiers is ordinary
// shadowing — higher tier wins, no error, and the loser is gone from the
// effective catalog.
func TestCrossTierShadows(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(
		staticContributor("plugin", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopePlugin, "pa", "plugin"), Payload: "from-plugin"}),
		staticContributor("project", Contribution{Kind: KindCommand, ID: "deploy", Source: src(ScopeProject, "", "project"), Payload: "from-project"}),
	)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	winners := snap.Catalog().Get(KindCommand, "deploy")
	if len(winners) != 1 {
		t.Fatalf("effective catalog holds %d deploy commands, want 1 winner", len(winners))
	}
	if winners[0].Payload != "from-project" {
		t.Fatalf("winner payload = %v, want the project-tier contribution", winners[0].Payload)
	}
	if winners[0].Source.Scope != ScopeProject {
		t.Fatalf("winner scope = %s, want project", winners[0].Source.Scope)
	}
}

// TestBuildValidationErrors exercises the per-kind ID shape checks: every
// malformed contribution must be rejected before resolution.
func TestBuildValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		contrib Contribution
		want    string
	}{
		{"empty id", Contribution{Kind: KindTool, Source: src(ScopeBuiltin, "", "builtin"), Payload: schemaPayload("x", "x")}, "empty ID"},
		{"unknown kind", Contribution{Kind: "wat", ID: "x", Source: src(ScopeBuiltin, "", "builtin")}, "unknown kind"},
		{"uppercase tool", Contribution{Kind: KindTool, ID: "ReadFile", Source: src(ScopeBuiltin, "", "builtin"), Payload: schemaPayload("ReadFile", "x")}, "lowercase"},
		{"malformed mcp id", Contribution{Kind: KindTool, ID: "mcp__bad", Source: src(ScopeBuiltin, "", "builtin"), Payload: schemaPayload("mcp__bad", "x")}, "mcp__<server>__<tool>"},
		{"mcp payload without prefix", Contribution{Kind: KindTool, ID: "plain", Source: src(ScopeBuiltin, "", "builtin"), Payload: fakeMCPTool{name: "plain"}}, "must start with mcp__"},
		{"bad tool payload", Contribution{Kind: KindTool, ID: "plain", Source: src(ScopeBuiltin, "", "builtin"), Payload: 42}, "payload"},
		{"bad provider ref", Contribution{Kind: KindProvider, ID: "openai", Source: src(ScopeBuiltin, "", "builtin"), Payload: provider.Descriptor{Ref: "openai"}}, "<name>/<model>"},
		{"unknown scope", Contribution{Kind: KindSkill, ID: "s", Source: ContributionSource{Scope: "moon", Origin: "x"}}, "unknown scope"},
		{"whitespace id", Contribution{Kind: KindSkill, ID: "a b", Source: src(ScopeGlobal, "", "user")}, "whitespace"},
		{"unknown point", Contribution{Kind: KindInterceptor, ID: "tool.middle", Source: src(ScopePlugin, "p", "plugin")}, "unknown interceptor point"},
		{"priority out of range", Contribution{Kind: KindInterceptor, ID: string(PointToolAfter), Priority: 5000, Source: src(ScopePlugin, "p", "plugin")}, "out of range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder()
			b.AddContributor(staticContributor("bad", tc.contrib))
			_, _, err := b.Build(context.Background())
			if err == nil {
				t.Fatalf("Build succeeded, want validation error containing %q", tc.want)
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("error %v is not a *ValidationError", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestContributorErrorPropagates: a failing discovery source must fail the
// build — a half-built snapshot is worse than none.
func TestContributorErrorPropagates(t *testing.T) {
	boom := ContributorFunc{
		ContributorName: "boom",
		Fn:              func(context.Context) ([]Contribution, error) { return nil, errors.New("disk exploded") },
	}
	b := NewBuilder()
	b.AddContributor(boom)
	_, _, err := b.Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "disk exploded") {
		t.Fatalf("Build error = %v, want contributor name + cause", err)
	}
}

// TestActivatorSeam: the default activator binds an empty set to the snapshot
// generation; a custom activator observes the frozen snapshot.
func TestActivatorSeam(t *testing.T) {
	snap, set, err := NewBuilder().WithGeneration(42).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if set.Generation() != snap.Generation() || set.Generation() != 42 {
		t.Fatalf("set generation = %d, want 42", set.Generation())
	}
	if set.Len() != 0 {
		t.Fatalf("default set holds %d closers, want 0", set.Len())
	}

	var observed *RuntimeSnapshot
	custom := NewBuilder().WithGeneration(9).WithActivator(func(_ context.Context, s *RuntimeSnapshot) (*RuntimeSet, error) {
		observed = s
		return nil, nil // nil set must become an empty set, not a nil dereference
	})
	snap2, set2, err := custom.Build(context.Background())
	if err != nil {
		t.Fatalf("custom Build: %v", err)
	}
	if observed != snap2 {
		t.Fatal("activator did not receive the built snapshot")
	}
	if set2 == nil || set2.Generation() != 9 {
		t.Fatalf("nil activator result handled wrongly: %+v", set2)
	}
}

// fakeMCPTool is an MCP-backed tool payload: it must be namespaced under
// mcp__ or validation rejects it.
type fakeMCPTool struct{ name string }

func (f fakeMCPTool) Name() string            { return f.name }
func (f fakeMCPTool) Description() string     { return "fake mcp tool" }
func (f fakeMCPTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f fakeMCPTool) ReadOnly() bool          { return true }
func (f fakeMCPTool) MCPServerName() string   { return "srv" }
func (f fakeMCPTool) MCPRawToolName() string  { return "raw" }
func (f fakeMCPTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

// TestMCPToolNamespacedAccepted: the same MCP payload passes once its ID
// carries the required namespace.
func TestMCPToolNamespacedAccepted(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(staticContributor("mcp",
		Contribution{Kind: KindTool, ID: "mcp__srv__raw", Source: src(ScopePlugin, "p", "plugin"), Payload: fakeMCPTool{name: "mcp__srv__raw"}}),
	)
	snap, _, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(snap.ToolSchemas()) != 1 || snap.ToolSchemas()[0].Name != "mcp__srv__raw" {
		t.Fatalf("schemas = %+v, want the namespaced MCP tool", snap.ToolSchemas())
	}
}
