package delegation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"strings"
)

type TeamTool struct{ task *TaskTool }

type teamRequest struct {
	taskContextArgs
	Action string   `json:"action"`
	Name   string   `json:"name"`
	Prompt string   `json:"prompt"`
	Tools  []string `json:"tools"`
	Model  string   `json:"model"`
}

func NewTeamTool(task *TaskTool) *TeamTool { return &TeamTool{task: task} }
func (*TeamTool) Name() string             { return "team" }
func (*TeamTool) ReadOnly() bool           { return false }
func (*TeamTool) Reach() tool.Reach        { return tool.ReachHostControl }
func (*TeamTool) Description() string {
	return "Create reusable named collaborators only when useful. List members, create one for a task, send follow-up work to its retained transcript, or retire a finished member. Only the root conversation manages its own members. Reports use reply_to_parent. Membership grants no extra permissions; tools and model stay compatible on reuse."
}
func (*TeamTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","send","retire"]},"name":{"type":"string"},"prompt":{"type":"string"},"tools":{"type":"array","items":{"type":"string"}},"model":{"type":"string"},"context_summary":{"type":"string"},"context_refs":{"type":"array","items":{"type":"string"},"maxItems":64}},"required":["action"],"additionalProperties":false}`)
}

func (t *TeamTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var p teamRequest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return "", err
	}
	if d.Decode(new(any)) != io.EOF {
		return "", fmt.Errorf("expected one team request")
	}
	if agent.SubagentDepth(ctx) != 0 || agent.ParentSession(ctx) == "" || t.task == nil || t.task.transcripts == nil {
		return "", fmt.Errorf("team management requires the owning root conversation")
	}
	parent := agent.ParentSession(ctx)
	p.Name = strings.TrimSpace(p.Name)
	if err := p.validate(); err != nil {
		return "", err
	}
	s := t.task.transcripts
	if p.Action == "create" {
		release, err := s.reserveMember(parent, p.Name)
		if err != nil {
			return "", err
		}
		defer release()
	}
	members, selected, err := s.teamMembers(parent, t.task.workspaceRoot, p.Name)
	if err != nil {
		return "", err
	}
	if p.Action == "list" {
		data, err := json.Marshal(members)
		return string(data), err
	}
	if p.Action == "create" && selected != nil {
		return "", fmt.Errorf("member already exists; use send")
	}
	if p.Action != "create" && selected == nil {
		return "", fmt.Errorf("member not found in this conversation")
	}
	if p.Action == "retire" {
		return "Member retired; its transcript remains available.", s.retireMember(selected.Ref)
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if p.Action == "send" && (p.Model != "" || len(p.Tools) > 0) {
		return "", fmt.Errorf("send preserves the member's model and tools; create another member to change them")
	}
	if selected != nil && selected.State == RunRunning {
		return "Guidance queued for member; it may not have read it yet.", s.Steer(selected.Ref, parent, t.task.workspaceRoot, p.Prompt, SteerFromParent, GuidanceContext{Summary: p.ContextSummary, ResultRefs: p.ContextRefs})
	}
	ref, effort := "", ""
	if selected != nil {
		meta, err := s.LoadMeta(selected.Ref)
		if err != nil {
			return "", err
		}
		ref, p.Tools, p.Model, effort = selected.Ref, meta.ToolScope, meta.Model, meta.Effort
	}
	spec, err := t.task.buildTaskSpec(ctx, p.Prompt, p.Name, "", nil, p.Tools, 0, p.Model, effort, ref, "", false, false)
	if err != nil {
		return "", err
	}
	spec.Worker.Kind, spec.Worker.Name = "member", p.Name
	p.apply(&spec)
	return t.task.RunProfileSpec(ctx, spec)
}

func (p teamRequest) validate() error {
	switch p.Action {
	case "list":
		return nil
	case "create", "send", "retire":
		if p.Name == "" || len(p.Name) > 80 || strings.ContainsAny(p.Name, "\r\n") {
			return fmt.Errorf("member name must contain 1 to 80 bytes on one line")
		}
		return nil
	default:
		return fmt.Errorf("unknown team action")
	}
}

func (s *SubagentStore) reserveMember(parent, name string) (func(), error) {
	key := parent + "\x00" + name
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.members == nil {
		s.members = map[string]bool{}
	}
	if s.members[key] {
		return nil, fmt.Errorf("member operation already in progress")
	}
	s.members[key] = true
	return func() { s.mu.Lock(); delete(s.members, key); s.mu.Unlock() }, nil
}
