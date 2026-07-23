package cli

import (
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/jobs"
)

type machineTask struct {
	ID               string `json:"id"`
	SessionID        string `json:"session_id"`
	Kind             string `json:"kind"`
	Status           string `json:"status"`
	StartedAt        string `json:"started_at"`
	FinishedAt       string `json:"finished_at,omitempty"`
	ArtifactComplete bool   `json:"artifact_complete"`
}

type machineTaskList struct {
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command"`
	Tasks         []machineTask `json:"tasks"`
}

type machineTaskShow struct {
	SchemaVersion int         `json:"schema_version"`
	Command       string      `json:"command"`
	Task          machineTask `json:"task"`
}

type taskMachineOptions struct {
	dir       string
	sessionID string
	target    string
	json      bool
}

func taskCommand(args []string) int {
	return runTaskCommand(args, os.Stdout)
}

func runTaskCommand(args []string, out io.Writer) int {
	command := "task"
	if len(args) == 0 {
		return writeMachineError(out, command, "invalid_argument", "a task operation is required")
	}
	operation := args[0]
	command = "task." + operation
	if operation != "list" && operation != "show" {
		return writeMachineError(out, command, "unknown_command", "unknown task operation")
	}
	options, code, message := parseTaskMachineOptions(args[1:], operation)
	if code != "" {
		return writeMachineError(out, command, code, message)
	}
	if !options.json {
		return writeMachineError(out, command, "invalid_argument", "--json is required")
	}
	if options.dir == "" {
		options.dir = resolveCLISessionDir()
	}
	identityKey, err := loadMachineIdentityKey()
	if err != nil {
		return writeMachineError(out, command, "machine_identity_unavailable", "machine identity is unavailable")
	}
	tasks, err := machineTasks(options.dir, options.sessionID, identityKey)
	if err != nil {
		return writeMachineError(out, command, "task_state_unavailable", "task state is unavailable")
	}
	if operation == "list" {
		return writeMachineJSON(out, machineTaskList{SchemaVersion: machineSchemaVersion, Command: command, Tasks: tasks})
	}
	var found *machineTask
	for i := range tasks {
		if tasks[i].ID != options.target {
			continue
		}
		if found != nil {
			return writeMachineError(out, command, "task_ambiguous", "task identifier is ambiguous")
		}
		found = &tasks[i]
	}
	if found == nil {
		return writeMachineError(out, command, "task_not_found", "task was not found")
	}
	return writeMachineJSON(out, machineTaskShow{SchemaVersion: machineSchemaVersion, Command: command, Task: *found})
}

func parseTaskMachineOptions(args []string, operation string) (taskMachineOptions, string, string) {
	var options taskMachineOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			options.json = true
		case "--dir":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return options, "invalid_argument", "--dir requires a value"
			}
			i++
			options.dir = args[i]
		case "--session":
			if i+1 >= len(args) || !validMachineID(args[i+1]) {
				return options, "invalid_argument", "--session requires a valid identifier"
			}
			i++
			options.sessionID = args[i]
		case "--help", "-h":
			return options, "invalid_argument", "use the documented machine interface"
		default:
			arg := strings.TrimSpace(args[i])
			if strings.HasPrefix(arg, "-") {
				return options, "invalid_argument", "unknown task option"
			}
			if operation == "list" || options.target != "" || !validMachineID(arg) {
				return options, "invalid_argument", "invalid task identifier"
			}
			options.target = arg
		}
	}
	if operation == "show" && options.target == "" {
		return options, "invalid_argument", "a task identifier is required"
	}
	return options, "", ""
}

func machineTasks(dir, sessionFilter string, identityKey []byte) ([]machineTask, error) {
	ordered, err := agent.ListSessionOrder(dir)
	if err != nil {
		return nil, err
	}
	out := make([]machineTask, 0)
	for _, session := range ordered {
		rawSessionID := agent.BranchID(session.Path)
		sessionID := machineSessionIDWithKey(rawSessionID, identityKey)
		if sessionFilter != "" && sessionID != sessionFilter {
			continue
		}
		sessionActive := agent.SessionLeaseHeld(session.Path)
		views, err := jobs.ListArtifactViews(session.Path)
		if err != nil {
			return nil, err
		}
		for _, view := range views {
			if view.Kind != "task" {
				continue
			}
			status := view.Status
			finishedAt := machineUnixMillis(view.FinishedAt)
			artifactComplete := view.ArtifactComplete
			if status == jobs.Running && !sessionActive {
				status = jobs.Interrupted
				finishedAt = ""
				artifactComplete = false
			}
			out = append(out, machineTask{
				ID:               view.ID,
				SessionID:        sessionID,
				Kind:             "background",
				Status:           string(status),
				StartedAt:        machineUnixMillis(view.StartedAt),
				FinishedAt:       finishedAt,
				ArtifactComplete: artifactComplete,
			})
		}
		artifacts, err := agent.ListSubagentsByParent(dir, rawSessionID)
		if err != nil {
			return nil, err
		}
		for _, artifact := range artifacts {
			if artifact.Meta.Kind != "task" {
				continue
			}
			status := artifact.Meta.Status
			finishedAt := ""
			artifactComplete := false
			if status == agent.SubagentRunning {
				if !sessionActive {
					status = agent.SubagentInterrupted
				}
			} else {
				finishedAt = machineTime(artifact.Meta.UpdatedAt)
				artifactComplete = machineArtifactComplete(artifact.SessionPath)
			}
			out = append(out, machineTask{
				ID:               artifact.Ref,
				SessionID:        sessionID,
				Kind:             "subagent",
				Status:           string(status),
				StartedAt:        machineTime(artifact.Meta.CreatedAt),
				FinishedAt:       finishedAt,
				ArtifactComplete: artifactComplete,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt > out[j].StartedAt
		}
		if out[i].SessionID != out[j].SessionID {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func machineArtifactComplete(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func validMachineID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, `/\\`)
}

func machineUnixMillis(value int64) string {
	if value <= 0 {
		return ""
	}
	return machineTime(time.UnixMilli(value))
}
