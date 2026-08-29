package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/agentview"
	"reasonix/internal/tool"
)

var (
	agentViewMu      sync.Mutex
	agentViewManager *agentview.Manager
	agentViewSession string
)

// SetAgentViewManager sets the agent view manager and current session ID for
// built-in agent view tools. The manager and session ID are used by agent view
// tools like agent_view_list and agent_view_update to interact with background
// agent sessions.
func SetAgentViewManager(m *agentview.Manager, sessionID string) {
	agentViewMu.Lock()
	defer agentViewMu.Unlock()
	agentViewManager = m
	agentViewSession = sessionID
}

func getAgentViewContext() (*agentview.Manager, string, bool) {
	agentViewMu.Lock()
	defer agentViewMu.Unlock()
	if agentViewManager == nil {
		return nil, "", false
	}
	return agentViewManager, agentViewSession, true
}

func init() {
	tool.RegisterBuiltin(agentViewListTool{})
	tool.RegisterBuiltin(agentViewUpdateTool{})
}

type agentViewListTool struct{}

func (agentViewListTool) Name() string { return "agent_view_list" }

func (agentViewListTool) Description() string {
	return "List all background agent sessions managed by agent view. Shows session state, summary, last activity, and workspace. Use this to see what other agents are working on."
}

func (agentViewListTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "state":{
    "type":"string",
    "description":"Optional filter: only show sessions in this state (working, needs_input, idle, completed, failed, stopped, sleeping).",
    "enum":["working","needs_input","idle","completed","failed","stopped","sleeping"]
  },
  "workspace":{
    "type":"string",
    "description":"Optional filter: only show sessions in this workspace directory."
  }
}
}`)
}

func (agentViewListTool) ReadOnly() bool { return true }

func (agentViewListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, _, ok := getAgentViewContext()
	if !ok {
		return "", fmt.Errorf("agent view is not available")
	}

	var p struct {
		State     string `json:"state"`
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var sessions []agentview.SessionInfo
	if p.State != "" {
		sessions = mgr.ByState(agentview.SessionState(p.State))
	} else if p.Workspace != "" {
		sessions = mgr.ListByWorkspace(p.Workspace)
	} else {
		sessions = mgr.List()
	}

	if len(sessions) == 0 {
		return "No background sessions found.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Background sessions (%d total):\n\n", len(sessions))
	for _, s := range sessions {
		pinned := ""
		if s.Pinned {
			pinned = " [pinned]"
		}
		fmt.Fprintf(&sb, "• %s (%s)%s\n", s.Name, s.State, pinned)
		fmt.Fprintf(&sb, "  ID: %s\n", s.ID)
		if s.Summary != "" {
			summary := s.Summary
			if len(summary) > 100 {
				summary = summary[:97] + "..."
			}
			fmt.Fprintf(&sb, "  Status: %s\n", summary)
		}
		fmt.Fprintf(&sb, "  Workspace: %s\n", s.Workspace)
		if s.Model != "" {
			fmt.Fprintf(&sb, "  Model: %s\n", s.Model)
		}
		fmt.Fprintf(&sb, "  Last active: %s ago\n", formatDuration(s.LastActivity))
		if len(s.PullRequests) > 0 {
			fmt.Fprintf(&sb, "  Pull requests: %s\n", strings.Join(s.PullRequests, ", "))
		}
		fmt.Fprintln(&sb)
	}

	return sb.String(), nil
}

type agentViewUpdateTool struct{}

func (agentViewUpdateTool) Name() string { return "agent_view_update" }

func (agentViewUpdateTool) Description() string {
	return "Update the current session's status in agent view. Use this to update the summary text or state that appears in the agent view list."
}

func (agentViewUpdateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "summary":{
    "type":"string",
    "description":"Short summary of what this session is doing or has done."
  },
  "state":{
    "type":"string",
    "description":"New state for the session.",
    "enum":["working","needs_input","idle","completed","failed"]
  }
}
}`)
}

func (agentViewUpdateTool) ReadOnly() bool { return true }

func (agentViewUpdateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, sessionID, ok := getAgentViewContext()
	if !ok {
		return "", fmt.Errorf("agent view is not available")
	}

	var p struct {
		Summary string `json:"summary"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	if p.State != "" {
		mgr.UpdateState(sessionID, agentview.SessionState(p.State))
	}
	if p.Summary != "" {
		mgr.UpdateSummary(sessionID, p.Summary)
	}

	return "Session status updated.", nil
}

func formatDuration(t interface{}) string {
	return "unknown"
}
