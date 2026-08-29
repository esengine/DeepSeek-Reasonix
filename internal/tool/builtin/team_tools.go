package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/agentteam"
	"reasonix/internal/tool"
)

var (
	teamToolMu      sync.Mutex
	teamToolManager *agentteam.Manager
	teamToolCurrent string
)

// SetTeamToolManager sets the team tool manager and current team name for
// built-in team tools. The manager and team name are used by team tools like
// send_message, task_create, etc. to operate on the correct agent team.
func SetTeamToolManager(m *agentteam.Manager, teamName string) {
	teamToolMu.Lock()
	defer teamToolMu.Unlock()
	teamToolManager = m
	teamToolCurrent = teamName
}

func getTeamContext() (*agentteam.Manager, string, bool) {
	teamToolMu.Lock()
	defer teamToolMu.Unlock()
	if teamToolManager == nil || teamToolCurrent == "" {
		return nil, "", false
	}
	return teamToolManager, teamToolCurrent, true
}

func init() {
	tool.RegisterBuiltin(sendMessageTool{})
	tool.RegisterBuiltin(taskListTool{})
	tool.RegisterBuiltin(taskCreateTool{})
	tool.RegisterBuiltin(taskGetTool{})
	tool.RegisterBuiltin(taskUpdateTool{})
	tool.RegisterBuiltin(teamMemberListTool{})
}

type sendMessageTool struct{}

func (sendMessageTool) Name() string { return "send_message" }

func (sendMessageTool) Description() string {
	return "Send a message to a teammate or all team members in the agent team. Use this to communicate with other agents working on the same team. You can send messages to specific teammates by name or broadcast to everyone."
}

func (sendMessageTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "to":{
    "type":"string",
    "description":"The teammate name to send the message to, or 'all' to broadcast to everyone."
  },
  "subject":{
    "type":"string",
    "description":"Short subject line for the message."
  },
  "content":{
    "type":"string",
    "description":"The message body to send."
  },
  "message_type":{
    "type":"string",
    "description":"Optional message type: 'chat', 'status_update', 'question', 'answer'. Defaults to 'chat'.",
    "enum":["chat","status_update","question","answer"]
  }
},
"required":["to","content"]
}`)
}

func (sendMessageTool) ReadOnly() bool { return true }

func (sendMessageTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, teamName, ok := getTeamContext()
	if !ok {
		return "", fmt.Errorf("not part of an agent team")
	}

	var p struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Content string `json:"content"`
		Type    string `json:"message_type"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.To) == "" {
		return "", fmt.Errorf("recipient is required")
	}
	if strings.TrimSpace(p.Content) == "" {
		return "", fmt.Errorf("message content is required")
	}
	if p.Type == "" {
		p.Type = "chat"
	}

	mbox, err := mgr.GetMailbox(teamName)
	if err != nil {
		return "", fmt.Errorf("get mailbox: %w", err)
	}

	msgID, err := mbox.Send("self", p.To, p.Subject, p.Content, p.Type)
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}

	return fmt.Sprintf("Message sent to %q (ID: %s).", p.To, msgID), nil
}

type taskListTool struct{}

func (taskListTool) Name() string { return "task_list" }

func (taskListTool) Description() string {
	return "List all tasks in the shared team task list. Shows pending, in-progress, and completed tasks with their status, assignee, and dependencies."
}

func (taskListTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "status":{
    "type":"string",
    "description":"Optional filter: only show tasks with this status (pending, in_progress, completed, failed, cancelled).",
    "enum":["pending","in_progress","completed","failed","cancelled"]
  },
  "assignee":{
    "type":"string",
    "description":"Optional filter: only show tasks assigned to this teammate."
  }
}
}`)
}

func (taskListTool) ReadOnly() bool { return true }

func (taskListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, teamName, ok := getTeamContext()
	if !ok {
		return "", fmt.Errorf("not part of an agent team")
	}

	var p struct {
		Status   string `json:"status"`
		Assignee string `json:"assignee"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	tl, err := mgr.GetTaskList(teamName)
	if err != nil {
		return "", fmt.Errorf("get task list: %w", err)
	}

	var tasks []agentteam.Task
	if p.Status != "" {
		tasks = tl.ByStatus(agentteam.TaskStatus(p.Status))
	} else if p.Assignee != "" {
		tasks = tl.ByAssignee(p.Assignee)
	} else {
		tasks = tl.List()
	}

	if len(tasks) == 0 {
		return "No tasks found.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Team tasks (%d total):\n\n", len(tasks))
	for _, t := range tasks {
		assignee := t.Assignee
		if assignee == "" {
			assignee = "unassigned"
		}
		fmt.Fprintf(&sb, "[%s] %s (ID: %s)\n",
			t.Status, t.Title, t.ID)
		fmt.Fprintf(&sb, "  Assignee: %s\n", assignee)
		if len(t.Dependencies) > 0 {
			fmt.Fprintf(&sb, "  Dependencies: %s\n", strings.Join(t.Dependencies, ", "))
		}
		if t.Description != "" {
			desc := t.Description
			if len(desc) > 100 {
				desc = desc[:97] + "..."
			}
			fmt.Fprintf(&sb, "  %s\n", desc)
		}
		fmt.Fprintln(&sb)
	}

	return sb.String(), nil
}

type taskCreateTool struct{}

func (taskCreateTool) Name() string { return "task_create" }

func (taskCreateTool) Description() string {
	return "Create a new task in the shared team task list. Tasks can be claimed by teammates or assigned explicitly. Use dependencies to specify tasks that must be completed first."
}

func (taskCreateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "title":{
    "type":"string",
    "description":"Short title for the task."
  },
  "description":{
    "type":"string",
    "description":"Detailed description of what needs to be done."
  },
  "assignee":{
    "type":"string",
    "description":"Optional teammate name to assign this task to. If omitted, the task is unassigned and can be claimed by anyone."
  },
  "dependencies":{
    "type":"array",
    "items":{"type":"string"},
    "description":"Optional list of task IDs that must be completed before this task can be started."
  },
  "priority":{
    "type":"integer",
    "description":"Task priority (higher = more important). Defaults to 0.",
    "minimum":0
  },
  "tags":{
    "type":"array",
    "items":{"type":"string"},
    "description":"Optional tags for categorizing tasks."
  }
},
"required":["title"]
}`)
}

func (taskCreateTool) ReadOnly() bool { return true }

func (taskCreateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, teamName, ok := getTeamContext()
	if !ok {
		return "", fmt.Errorf("not part of an agent team")
	}

	var p struct {
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Assignee     string   `json:"assignee"`
		Dependencies []string `json:"dependencies"`
		Priority     int      `json:"priority"`
		Tags         []string `json:"tags"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Title) == "" {
		return "", fmt.Errorf("task title is required")
	}

	tl, err := mgr.GetTaskList(teamName)
	if err != nil {
		return "", fmt.Errorf("get task list: %w", err)
	}

	status := agentteam.TaskPending
	if p.Assignee != "" {
		status = agentteam.TaskInProgress
	}

	task := agentteam.Task{
		Title:        p.Title,
		Description:  p.Description,
		Status:       status,
		Assignee:     p.Assignee,
		Dependencies: p.Dependencies,
		Priority:     p.Priority,
		Tags:         p.Tags,
	}

	id, err := tl.Create(task)
	if err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}

	return fmt.Sprintf("Task created: %q (ID: %s). Status: %s.", p.Title, id, status), nil
}

type taskGetTool struct{}

func (taskGetTool) Name() string { return "task_get" }

func (taskGetTool) Description() string {
	return "Get full details for a specific task by ID, including description, status, assignee, and output."
}

func (taskGetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "task_id":{
    "type":"string",
    "description":"The ID of the task to retrieve."
  }
},
"required":["task_id"]
}`)
}

func (taskGetTool) ReadOnly() bool { return true }

func (taskGetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, teamName, ok := getTeamContext()
	if !ok {
		return "", fmt.Errorf("not part of an agent team")
	}

	var p struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.TaskID) == "" {
		return "", fmt.Errorf("task ID is required")
	}

	tl, err := mgr.GetTaskList(teamName)
	if err != nil {
		return "", fmt.Errorf("get task list: %w", err)
	}

	task, ok := tl.Get(p.TaskID)
	if !ok {
		return "", fmt.Errorf("task %q not found", p.TaskID)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Task: %s\n", task.Title)
	fmt.Fprintf(&sb, "ID: %s\n", task.ID)
	fmt.Fprintf(&sb, "Status: %s\n", task.Status)
	assignee := task.Assignee
	if assignee == "" {
		assignee = "unassigned"
	}
	fmt.Fprintf(&sb, "Assignee: %s\n", assignee)
	fmt.Fprintf(&sb, "Created: %s\n", task.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "Updated: %s\n", task.UpdatedAt.Format("2006-01-02 15:04:05"))
	if len(task.Dependencies) > 0 {
		fmt.Fprintf(&sb, "Dependencies: %s\n", strings.Join(task.Dependencies, ", "))
	}
	if task.Priority > 0 {
		fmt.Fprintf(&sb, "Priority: %d\n", task.Priority)
	}
	if len(task.Tags) > 0 {
		fmt.Fprintf(&sb, "Tags: %s\n", strings.Join(task.Tags, ", "))
	}
	if task.Description != "" {
		fmt.Fprintf(&sb, "\nDescription:\n%s\n", task.Description)
	}
	if task.Output != "" {
		fmt.Fprintf(&sb, "\nOutput:\n%s\n", task.Output)
	}

	return sb.String(), nil
}

type taskUpdateTool struct{}

func (taskUpdateTool) Name() string { return "task_update" }

func (taskUpdateTool) Description() string {
	return "Update a task's status, assignee, description, or output. Use this to mark tasks as complete, reassign them, or add results."
}

func (taskUpdateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "task_id":{
    "type":"string",
    "description":"The ID of the task to update."
  },
  "status":{
    "type":"string",
    "description":"New status for the task.",
    "enum":["pending","in_progress","completed","failed","cancelled"]
  },
  "assignee":{
    "type":"string",
    "description":"New assignee for the task."
  },
  "description":{
    "type":"string",
    "description":"Updated description."
  },
  "output":{
    "type":"string",
    "description":"Task results or output."
  },
  "title":{
    "type":"string",
    "description":"Updated task title."
  }
},
"required":["task_id"]
}`)
}

func (taskUpdateTool) ReadOnly() bool { return true }

func (taskUpdateTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, teamName, ok := getTeamContext()
	if !ok {
		return "", fmt.Errorf("not part of an agent team")
	}

	var p struct {
		TaskID      string `json:"task_id"`
		Status      string `json:"status"`
		Assignee    string `json:"assignee"`
		Description string `json:"description"`
		Output      string `json:"output"`
		Title       string `json:"title"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.TaskID) == "" {
		return "", fmt.Errorf("task ID is required")
	}

	tl, err := mgr.GetTaskList(teamName)
	if err != nil {
		return "", fmt.Errorf("get task list: %w", err)
	}

	updates := agentteam.Task{}
	if p.Status != "" {
		updates.Status = agentteam.TaskStatus(p.Status)
	}
	if p.Assignee != "" {
		updates.Assignee = p.Assignee
	}
	if p.Description != "" {
		updates.Description = p.Description
	}
	if p.Output != "" {
		updates.Output = p.Output
	}
	if p.Title != "" {
		updates.Title = p.Title
	}

	if err := tl.Update(p.TaskID, updates); err != nil {
		return "", fmt.Errorf("update task: %w", err)
	}

	return fmt.Sprintf("Task %s updated successfully.", p.TaskID), nil
}

type teamMemberListTool struct{}

func (teamMemberListTool) Name() string { return "team_member_list" }

func (teamMemberListTool) Description() string {
	return "List all teammates in the current agent team, including their names, roles, status, and model information."
}

func (teamMemberListTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{}
}`)
}

func (teamMemberListTool) ReadOnly() bool { return true }

func (teamMemberListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	mgr, teamName, ok := getTeamContext()
	if !ok {
		return "", fmt.Errorf("not part of an agent team")
	}

	team, ok := mgr.GetTeam(teamName)
	if !ok {
		return "", fmt.Errorf("team %q not found", teamName)
	}

	members := team.Members()
	if len(members) == 0 {
		return "No teammates found in this team.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Team %q has %d members:\n\n", teamName, len(members))
	for _, m := range members {
		role := m.Role
		if role == "" {
			role = "teammate"
		}
		status := m.Status
		if status == "" {
			status = "active"
		}
		model := m.Model
		if model == "" {
			model = "default"
		}
		fmt.Fprintf(&sb, "• %s (%s)\n", m.Name, role)
		fmt.Fprintf(&sb, "  ID: %s\n", m.ID)
		fmt.Fprintf(&sb, "  Status: %s\n", status)
		fmt.Fprintf(&sb, "  Model: %s\n", model)
		fmt.Fprintln(&sb)
	}

	return sb.String(), nil
}
