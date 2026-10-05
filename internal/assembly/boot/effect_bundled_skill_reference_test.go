package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
)

type bundledReferenceProvider struct {
	effectRecordingProvider
	references []string
}

func (*bundledReferenceProvider) Name() string { return "boot-bundled-reference" }

func (p *bundledReferenceProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	first := len(p.reqs) == 1
	p.mu.Unlock()
	ch := make(chan provider.Chunk, len(p.references)+1)
	if first {
		for i, reference := range p.references {
			args, err := json.Marshal(map[string]string{"path": reference})
			if err != nil {
				return nil, err
			}
			ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprintf("read-reference-%d", i), Name: "read_file", Arguments: string(args)}}
		}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "format read"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectBundledSkillReferenceReachesProviderAfterCopyInstall(t *testing.T) {
	for _, tc := range []struct {
		name       string
		skill      string
		references []string
	}{
		{name: "release-note-kit", skill: "release-note", references: []string{"format.md"}},
		{name: "issue-fix-kit", skill: "issue-fix", references: []string{"delivery.md", "scenario.md"}},
		{name: "frontend-page-kit", skill: "frontend-page", references: []string{"delivery.md", "scenario.md", "../fixture/brief.md", "../fixture/tickets.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := robustTempDir(t)
			if err := os.CopyFS(source, os.DirFS(filepath.Join("..", "..", "..", "examples", tc.name))); err != nil {
				t.Fatal(err)
			}
			wantReferences := make([][]byte, len(tc.references))
			for i, reference := range tc.references {
				body, err := os.ReadFile(filepath.Join(source, "skills", tc.skill, "references", reference))
				if err != nil {
					t.Fatal(err)
				}
				wantReferences[i] = body
			}
			home := isolateConfigHome(t)
			t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
			workspace := robustTempDir(t)
			t.Chdir(workspace)
			writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-bundled-reference-`+tc.name+`"
model = "x"
`)
			approveWorkspace(t, workspace)
			installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
			request := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
			for _, apply := range []bool{false, true} {
				request["apply"] = apply
				args, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				out, err := installer.Execute(t.Context(), args)
				if err != nil {
					t.Fatalf("install_source: %v", err)
				}
				var result struct {
					OK      bool   `json:"ok"`
					Applied bool   `json:"applied"`
					PlanID  string `json:"planId"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK || result.Applied != apply || result.PlanID == "" {
					t.Fatalf("install_source apply=%t: %s, err=%v", apply, out, err)
				}
				request["planId"] = result.PlanID
			}
			if err := os.RemoveAll(source); err != nil {
				t.Fatal(err)
			}
			rec := &bundledReferenceProvider{}
			provider.Register("boot-bundled-reference-"+tc.name, func(provider.Config) (provider.Provider, error) { return rec, nil })
			ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			skillPath := ""
			for _, sk := range ctrl.SlashSkills() {
				if sk.SlashName() == tc.name+":"+tc.skill {
					skillPath = sk.Path
				}
			}
			if skillPath == "" || strings.HasPrefix(skillPath, source+string(filepath.Separator)) {
				t.Fatalf("copied skill path = %q", skillPath)
			}
			for i, reference := range tc.references {
				path := filepath.Join(filepath.Dir(skillPath), "references", reference)
				rec.references = append(rec.references, path)
				if got, err := os.ReadFile(path); err != nil || string(got) != string(wantReferences[i]) {
					t.Fatalf("copied reference %s = %q, err=%v", reference, got, err)
				}
			}
			ctrl.Submit("/" + tc.name + ":" + tc.skill + " use the selected inputs")
			deadline := time.Now().Add(30 * time.Second)
			for ctrl.Running() {
				if time.Now().After(deadline) {
					t.Fatal("bundled skill turn did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			requests := rec.requests()
			if len(requests) != 2 {
				t.Fatalf("provider received %d requests, want invocation and reference result", len(requests))
			}
			if systemMessage(requests[0].Messages) != systemMessage(requests[1].Messages) {
				t.Fatal("reference reads changed the cache-stable prefix")
			}
			for _, req := range requests {
				for _, reference := range wantReferences {
					if strings.Contains(systemMessage(req.Messages), string(reference)) {
						t.Fatal("reference content leaked into the cache-stable prefix")
					}
				}
			}
			pinned := false
			read := map[string]string{}
			for _, message := range requests[1].Messages {
				pinned = pinned || message.Role == provider.RoleUser && strings.Contains(message.Content, "<skill-pin name=\""+tc.skill+"\">") && strings.Contains(message.Content, skillPath)
				if message.Role == provider.RoleTool {
					read[message.ToolCallID] = message.Content
				}
			}
			if !pinned {
				t.Fatal("copied skill/source did not reach provider")
			}
			for i, reference := range wantReferences {
				result := read[fmt.Sprintf("read-reference-%d", i)]
				if result == "" {
					t.Fatalf("reference %s did not reach provider", tc.references[i])
				}
				for line := range strings.SplitSeq(string(reference), "\n") {
					line = strings.TrimSpace(line)
					if line != "" && !strings.Contains(result, line) {
						t.Errorf("reference %s line missing from provider tool result: %q", tc.references[i], line)
					}
				}
			}
		})
	}
}
