package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/isolation"
)

type nestedContinuationProvider struct {
	background bool
	first      func() error
}

func (*nestedContinuationProvider) Name() string { return "nested-continuation" }

func callResult(req provider.Request, id string) (string, bool) {
	for _, message := range req.Messages {
		if message.Role == provider.RoleTool && message.ToolCallID == id {
			return message.Content, true
		}
	}
	return "", false
}

func (p *nestedContinuationProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	text := func() { ch <- provider.Chunk{Type: provider.ChunkText, Text: "retained continuation"} }
	switch {
	case strings.Contains(firstUser(req), "NEST-CONT-LEAF"):
		update := false
		for _, message := range req.Messages {
			update = update || (message.Role == provider.RoleUser && strings.Contains(message.Content, "NEST-CONT-UPDATE"))
		}
		_, read := callResult(req, "update-read")
		_, edited := callResult(req, "update-edit")
		_, created := callResult(req, "create-file")
		switch {
		case update && !read:
			call("update-read", "read_file", map[string]string{"path": "leaf.txt"})
		case update && !edited:
			call("update-edit", "edit_file", map[string]string{"path": "leaf.txt", "old_string": "first\n", "new_string": "second\n"})
		case !update && !created:
			call("create-file", "write_file", map[string]string{"path": "leaf.txt", "content": "first\n"})
		default:
			text()
		}
	case strings.Contains(firstUser(req), "NEST-CONT-PARENT"):
		if err := p.parent(req, call, text); err != nil {
			return nil, err
		}
	default:
		if !hasToolResult(req) {
			call("parent", "task", map[string]string{"prompt": "NEST-CONT-PARENT", "isolation": "worktree"})
		} else {
			text()
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *nestedContinuationProvider) parent(req provider.Request, call func(string, string, any), done func()) error {
	initial, created := callResult(req, "create-child")
	if !created {
		call("create-child", "task", map[string]any{"prompt": "NEST-CONT-LEAF", "isolation": "worktree", "run_in_background": p.background,
			"write_paths": []string{"leaf.txt"}, "tools": []string{"read_file", "write_file", "edit_file"}})
		return nil
	}
	if p.background {
		waited, collected := callResult(req, "wait-create")
		if !collected {
			return continuationWait(initial, "wait-create", call)
		}
		initial = waited
	}
	continued, resumed := callResult(req, "continue-child")
	if !resumed {
		ref := regexp.MustCompile(`sa_[A-Za-z0-9_]+`).FindString(initial)
		if ref == "" {
			return fmt.Errorf("nested result has no continuation reference: %s", initial)
		}
		if err := p.first(); err != nil {
			return err
		}
		call("continue-child", "task", map[string]any{"prompt": "NEST-CONT-UPDATE", "continue_from": ref, "run_in_background": p.background,
			"write_paths": []string{"leaf.txt"}, "tools": []string{"read_file", "write_file", "edit_file"}})
		return nil
	}
	if _, collected := callResult(req, "wait-continue"); p.background && !collected {
		return continuationWait(continued, "wait-continue", call)
	}
	done()
	return nil
}

func continuationWait(output, id string, call func(string, string, any)) error {
	var job string
	if _, err := fmt.Sscanf(output, "Started background task %q", &job); err != nil || job == "" {
		return fmt.Errorf("nested background start has no job identity: %s", output)
	}
	call(id, "wait", map[string]any{"job_ids": []string{job}})
	return nil
}

func TestEffectNestedContinuationKeepsEnvironmentAndAdvancesGeneration(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreground", true: "background"}[background], func(t *testing.T) {
			p := &nestedContinuationProvider{background: background}
			ctrl, root := nestedExecutionFixture(t, p, "", "", event.Discard)
			var firstID, firstGeneration string
			p.first = func() error {
				catalog, err := ctrl.IsolationResults(t.Context())
				if err != nil {
					return err
				}
				for _, row := range catalog.Results {
					if row.ParentID != "" && row.Kind == "result" {
						firstID, firstGeneration = row.ID, row.Generation
					}
				}
				return nil
			}
			if err := ctrl.Run(t.Context(), "exercise nested continuation"); err != nil {
				t.Fatal(err)
			}
			catalog, err := ctrl.IsolationResults(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var child *isolation.ResultSummary
			for index := range catalog.Results {
				row := &catalog.Results[index]
				if row.ParentID != "" && row.Kind == "result" {
					if child != nil {
						t.Fatalf("continuation created a replacement environment: %+v", catalog)
					}
					child = row
				}
			}
			if child == nil || child.ID != firstID || child.Generation == firstGeneration || firstGeneration == "" {
				t.Fatalf("real nested continuation lost its environment or generation: initial=%s/%s catalog=%+v", firstID, firstGeneration, catalog)
			}
			if raw, err := os.ReadFile(filepath.Join(child.CandidateRoot, "leaf.txt")); err != nil || string(raw) != "second\n" {
				t.Fatalf("continuation did not edit its retained candidate: %q %v", raw, err)
			}
			for _, source := range []string{root, child.TargetWorkspace} {
				if _, err := os.Stat(filepath.Join(source, "leaf.txt")); !os.IsNotExist(err) {
					t.Fatalf("unverified continuation wrote its source: %v", err)
				}
			}
		})
	}
}
