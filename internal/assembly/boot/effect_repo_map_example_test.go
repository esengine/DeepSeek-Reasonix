package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/frontmatter"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

const repoMapPromptStart = "Trace the task's repository question through the selected local files."

type repoMapExampleProvider struct {
	effectRecordingProvider
	files []string
}

func (*repoMapExampleProvider) Name() string { return "boot-repo-map-example" }

func (p *repoMapExampleProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, len(p.files)+1)
	if !strings.Contains(systemMessage(req.Messages), repoMapPromptStart) {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "parent acknowledged"}
	} else {
		results := 0
		for _, message := range req.Messages {
			if message.Role == provider.RoleTool && strings.HasPrefix(message.ToolCallID, "fixture-") {
				results++
			}
		}
		if results == len(p.files) {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "fixture inspected"}
		} else {
			for i, file := range p.files {
				args, err := json.Marshal(map[string]string{"path": file})
				if err != nil {
					return nil, err
				}
				ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprintf("fixture-%d", i), Name: "read_file", Arguments: string(args)}}
			}
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectRepoMapExampleLifecycle(t *testing.T) {
	example, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "repo-map-kit"))
	if err != nil {
		t.Fatal(err)
	}
	source := robustTempDir(t)
	if err := os.CopyFS(source, os.DirFS(example)); err != nil {
		t.Fatal(err)
	}
	rawProfile, err := os.ReadFile(filepath.Join(source, "agents", "map.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, profileBody := frontmatter.SplitLegacy(string(rawProfile))
	profileBody = strings.TrimSpace(profileBody)
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	if err := os.CopyFS(workspace, os.DirFS(filepath.Join(source, "fixture"))); err != nil {
		t.Fatal(err)
	}
	files := []string{"loader.go", "NOTES.md"}
	contents := make([]string, len(files))
	for i, file := range files {
		body, err := os.ReadFile(filepath.Join(workspace, file))
		if err != nil {
			t.Fatal(err)
		}
		contents[i] = string(body)
	}
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "REPO MAP PARENT BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-repo-map-example"
model = "x"
`)
	approveWorkspace(t, workspace)
	var rec *repoMapExampleProvider
	provider.Register("boot-repo-map-example", func(provider.Config) (provider.Provider, error) { return rec, nil })
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	type result struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		Applied bool   `json:"applied"`
		PlanID  string `json:"planId"`
	}
	install := func(args map[string]any) result {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var got result
		if err := json.Unmarshal([]byte(out), &got); err != nil || !got.OK {
			t.Fatalf("install_source = %s, err=%v", out, err)
		}
		return got
	}
	var prefix, schema string
	check := func(t *testing.T, present bool) {
		t.Helper()
		rec = &repoMapExampleProvider{files: files}
		ctrl, err := Build(t.Context(), Options{Sink: event.Discard, SessionDir: filepath.Join(robustTempDir(t), "sessions")})
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		const name = "repo-map-kit:agent:map"
		var profile skill.Skill
		found := false
		for _, sk := range ctrl.SlashSkills() {
			if sk.SlashName() == name {
				profile, found = sk, true
			}
		}
		if found != present {
			t.Fatalf("qualified profile found=%t, want %t", found, present)
		}
		if present && (profile.Plugin != "repo-map-kit" || profile.RunAs != skill.RunSubagent || profile.Invocation != "manual" || !profile.ReadOnly || profile.Model != "" || profile.Body != profileBody || !slices.Equal(profile.AllowedTools, []string{"read_file", "grep", "glob", "ls"})) {
			t.Fatalf("installed profile = %+v", profile)
		}
		if present && profile.Path != filepath.Join(pluginpkg.InstallRoot(reasonixHome, "repo-map-kit"), "agents", "map.md") {
			t.Fatalf("profile is not the managed copy: %s", profile.Path)
		}
		ctrl.EnsureSessionPath()
		const parentOnly = "PARENT-ONLY-CONTEXT: examine an unrelated question"
		if err := ctrl.Run(t.Context(), parentOnly); err != nil {
			t.Fatal(err)
		}
		parent := rec.requests()[0]
		encoded, err := json.Marshal(parent.Tools)
		if err != nil {
			t.Fatal(err)
		}
		if prefix == "" {
			prefix, schema = systemMessage(parent.Messages), string(encoded)
		} else if prefix != systemMessage(parent.Messages) || schema != string(encoded) {
			t.Fatal("package lifecycle changed the parent prefix or tool schema")
		}
		if strings.Contains(systemMessage(parent.Messages), profileBody) {
			t.Fatal("manual child profile entered the parent prefix")
		}
		const task = "Explain loader.go and compare NOTES.md; report what was not verified."
		answer, err := ctrl.RunSubagentProfile(t.Context(), name, task, false)
		if !present {
			if err == nil || len(rec.requests()) != 1 {
				t.Fatalf("absent profile invoked a provider: answer=%q, err=%v", answer, err)
			}
			return
		}
		if err != nil || !strings.HasSuffix(answer, "fixture inspected") {
			t.Fatalf("child answer = %q, err=%v", answer, err)
		}
		requests := rec.requests()
		if len(requests) != 3 {
			t.Fatalf("provider requests=%d, want parent + child read + child result", len(requests))
		}
		for _, req := range requests[1:] {
			if !strings.HasPrefix(systemMessage(req.Messages), profileBody) {
				t.Fatal("installed profile body did not reach the child")
			}
			if !slices.Equal(toolSchemaNames(req.Tools), []string{"complete_subtask", "glob", "grep", "ls", "read_file"}) {
				t.Fatalf("child tool ceiling = %v", toolSchemaNames(req.Tools))
			}
			var user string
			for _, message := range req.Messages {
				if strings.Contains(message.Content, parentOnly) {
					t.Fatal("child received unrelated parent conversation")
				}
				if message.Role == provider.RoleUser {
					user = message.Content
				}
			}
			if !strings.Contains(user, task) {
				t.Fatalf("child did not receive the explicit task: %s", user)
			}
		}
		for i, file := range files {
			var toolResult string
			for _, message := range requests[2].Messages {
				if message.Role == provider.RoleTool && message.ToolCallID == fmt.Sprintf("fixture-%d", i) {
					toolResult = message.Content
				}
			}
			for line := range strings.SplitSeq(contents[i], "\n") {
				if line = strings.TrimSpace(line); line != "" && !strings.Contains(toolResult, line) {
					t.Errorf("fixture %s line missing from provider tool result: %q", file, line)
				}
			}
			if got, err := os.ReadFile(filepath.Join(workspace, file)); err != nil || string(got) != contents[i] {
				t.Fatalf("fixture %s changed: %q, err=%v", file, got, err)
			}
		}
	}
	t.Run("absent", func(t *testing.T) { check(t, false) })
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	preview := install(args)
	if preview.Applied || preview.Status != "planned" || preview.PlanID == "" {
		t.Fatalf("preview = %+v", preview)
	}
	t.Run("preview", func(t *testing.T) { check(t, false) })
	args["apply"], args["planId"] = true, preview.PlanID
	if applied := install(args); !applied.Applied || applied.Status != "done" {
		t.Fatalf("applied = %+v", applied)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	t.Run("copied", func(t *testing.T) { check(t, true) })
	if err := pluginpkg.SetEnabled(reasonixHome, "repo-map-kit", false); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(pluginpkg.InstallRoot(reasonixHome, "repo-map-kit"), "agents", "map.md")); err != nil || string(got) != string(rawProfile) {
		t.Fatalf("disable changed the copied profile: %q, err=%v", got, err)
	}
	t.Run("disabled", func(t *testing.T) { check(t, false) })
	if err := pluginpkg.SetEnabled(reasonixHome, "repo-map-kit", true); err != nil {
		t.Fatal(err)
	}
	t.Run("reenabled", func(t *testing.T) { check(t, true) })
	if removed := install(map[string]any{"op": "uninstall", "name": "repo-map-kit", "scope": "global"}); !removed.Applied || removed.Status != "done" {
		t.Fatalf("removed = %+v", removed)
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(reasonixHome, "repo-map-kit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed profile survives removal: %v", err)
	}
	t.Run("removed", func(t *testing.T) { check(t, false) })
}
