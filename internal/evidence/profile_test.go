package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyEffectBashReadersAndWriters(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    EffectProfile
	}{
		{name: "status", command: "git status", want: EffectProfile{Known: true, ReadOnly: true, Reason: ReasonReadOnly}},
		{name: "diff", command: "git diff", want: EffectProfile{Known: true, ReadOnly: true, Reason: ReasonReadOnly}},
		{name: "push", command: "git push origin main", want: EffectProfile{Known: true, ExternalState: true, UsesNetwork: true, Reason: ReasonExternalState, Targets: []Target{{Kind: TargetExternal}}}},
		{name: "force push", command: "git push --force origin main", want: EffectProfile{Known: true, ExternalState: true, UsesNetwork: true, Destructive: true, Irreversible: true, Reason: ReasonExternalState, Targets: []Target{{Kind: TargetExternal}}}},
		{name: "unknown", command: "custom-tool --run", want: EffectProfile{WorkspaceWrite: true, Reason: "command effects are not statically known"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tt.command})
			got := ClassifyEffect(EffectInput{ToolName: "bash", Args: args})
			if got.Known != tt.want.Known || got.ReadOnly != tt.want.ReadOnly || got.WorkspaceWrite != tt.want.WorkspaceWrite ||
				got.ExternalState != tt.want.ExternalState || got.Destructive != tt.want.Destructive ||
				got.Irreversible != tt.want.Irreversible || got.UsesNetwork != tt.want.UsesNetwork {
				t.Fatalf("profile = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestClassifyEffectFileAndMCP(t *testing.T) {
	edit := ClassifyEffect(EffectInput{
		ToolName: "edit_file",
		Args:     json.RawMessage(`{"path":"internal/agent/agent.go"}`),
	})
	if !edit.Known || !edit.WorkspaceWrite || len(edit.Targets) != 1 || edit.Targets[0].Path != "internal/agent/agent.go" {
		t.Fatalf("edit profile = %+v", edit)
	}

	read := ClassifyEffect(EffectInput{
		ToolName:       "read_file",
		Args:           json.RawMessage(`{"path":"auth/session.go"}`),
		StaticReadOnly: true,
	})
	if !read.ReadOnly || read.MutatesState() {
		t.Fatalf("read-only file must not mutate: %+v", read)
	}

	mcpRead := ClassifyEffect(EffectInput{
		ToolName: "mcp__srv__get",
		Hint:     CallHint{Present: true, ReadOnly: true},
	})
	if !mcpRead.ReadOnly || mcpRead.MutatesState() {
		t.Fatalf("MCP read-only annotation = %+v", mcpRead)
	}

	opaque := ClassifyEffect(EffectInput{ToolName: "mcp__srv__write", Args: json.RawMessage(`{}`)})
	if opaque.Known || !opaque.OpaqueWriter() {
		t.Fatalf("unknown MCP writer must fail closed: %+v", opaque)
	}
}

func TestClassifyEffectKillShellIsHostStateOnly(t *testing.T) {
	profile := ClassifyEffect(EffectInput{ToolName: "kill_shell", Args: json.RawMessage(`{"job_id":"task-1"}`)})
	if !profile.Known || profile.ReadOnly || !profile.HostState || profile.WorkspaceWrite || profile.RepoMetadata || profile.ExternalState {
		t.Fatalf("kill_shell profile = %+v, want host-state-only mutation", profile)
	}
	effects := profile.ToolEffects()
	if !effects.StateMutation || effects.WorkspaceMutation || effects.ContentMutation || effects.RepositoryMutation {
		t.Fatalf("kill_shell effects = %+v, want state mutation without workspace mutation", effects)
	}
}

func TestClassifyWriteScopeScratchWriteFile(t *testing.T) {
	workspace := t.TempDir()
	scratchPath := filepath.Join(os.TempDir(), "reasonix-scope-probe.py")
	if got := ClassifyWriteScope(scratchPath, workspace, nil); got != WriteScopeScratch {
		t.Fatalf("write_file /tmp = %s, want scratch", got)
	}
	if got := ClassifyWriteScope("internal/agent/agent.go", workspace, nil); got != WriteScopeWorkspace {
		t.Fatalf("workspace edit = %s, want workspace", got)
	}
}

func TestClassifyEffectPrefersReceiptPaths(t *testing.T) {
	got := ClassifyEffect(EffectInput{
		ToolName:    "edit_file",
		Args:        json.RawMessage(`{"path":"expected.go"}`),
		ActualPaths: []string{"actual.go"},
	})
	if len(got.Targets) != 1 || got.Targets[0].Path != "actual.go" {
		t.Fatalf("receipt paths must win: %+v", got)
	}
}

func TestEffectProfileCloneCopiesTargets(t *testing.T) {
	orig := EffectProfile{Targets: []Target{{Path: "a.go", Kind: TargetFile}}}
	clone := orig.Clone()
	clone.Targets[0].Path = "b.go"
	if orig.Targets[0].Path != "a.go" {
		t.Fatal("clone must not share the target slice")
	}
}
