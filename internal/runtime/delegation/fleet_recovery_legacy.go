package delegation

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/execjournal"
	"reasonix/internal/state/sessionstore"
	"strings"
)

func (s *SubagentStore) legacyFleetPlan(path, parent, workspace string, meta sessionstore.SubagentMeta) ([]FleetRecoveryPlan, error) {
	group := ""
	for _, e := range execjournal.History(path) {
		if e.ID == meta.ExecutionID || e.ID == meta.ParentToolCallID {
			group = e.Group
		}
	}
	if group == "" {
		return []FleetRecoveryPlan{}, nil
	}
	view := FleetRecoveryPlan{ID: "legacy_" + bytesHash([]byte(group))[:20], Legacy: true, Tasks: []FleetRecoveryTask{}, Reason: "The original task arguments were not preserved; create a new plan explicitly."}
	for _, e := range execjournal.History(path) {
		if e.Group == group && execjournal.Live(path, e.ID) {
			view.Reason = "The original fleet is still running."
			return []FleetRecoveryPlan{view}, nil
		}
	}
	messages, _, intact, err := sessionstore.LoadSessionDisplayMessages(path)
	if err != nil {
		return nil, err
	}
	if !intact {
		return nil, sessionstore.ErrSessionDisplayReadModelDamaged
	}
	var calls []provider.ToolCall
	for _, message := range messages {
		if message.Role == provider.RoleAssistant {
			for _, call := range message.ToolCalls {
				if call.ID == group && call.Name == "fleet" {
					calls = append(calls, call)
				}
			}
		}
	}
	if len(calls) != 1 {
		return []FleetRecoveryPlan{view}, nil
	}
	params, err := decodeFleetParams(json.RawMessage(calls[0].Arguments))
	if err != nil || params.Recovery != nil {
		return []FleetRecoveryPlan{view}, nil
	}
	artifacts, err := sessionstore.ListSubagentsByParent(filepath.Dir(s.dir), parent)
	if err != nil {
		return nil, err
	}
	var resultDigests []string
	for i := range params.Tasks {
		item := &params.Tasks[i]
		var refs []string
		for _, a := range artifacts {
			if a.Meta.WorkspaceRoot == workspace && a.Meta.Status == sessionstore.SubagentCompleted && (a.Meta.ExecutionID == fleetNodeID(group, i) || a.Meta.ParentToolCallID == fleetNodeID(group, i)) {
				refs = append(refs, a.Ref)
			}
		}
		if len(refs) == 1 {
			*item = fleetTaskItem{ID: item.ID, Description: item.Description, DependsOn: item.DependsOn, AdoptRef: refs[0]}
		}
		state := "unfinished"
		if strings.TrimSpace(item.AdoptRef) != "" {
			answer, _, err := s.ReadFinalAnswer(item.AdoptRef, parent, workspace)
			if err != nil {
				return nil, err
			}
			resultDigests = append(resultDigests, bytesHash([]byte(answer)))
			state = "completed"
		} else {
			view.Ready = true
		}
		view.Tasks = append(view.Tasks, FleetRecoveryTask{ID: firstNonEmpty(item.ID, fmt.Sprint(i+1)), Description: item.Description, Prompt: item.Prompt, DependsOn: append([]string{}, item.DependsOn...), State: state, Ref: item.AdoptRef})
	}
	params.RunInBackground = false
	view.Arguments, err = json.Marshal(params)
	if err != nil {
		return nil, err
	}
	view.Snapshot = bytesHash(append([]byte(parent+"\x00"+workspace+"\x00"+strings.Join(resultDigests, ":")), view.Arguments...))
	view.Reason = "This older plan will be rebuilt under current permissions and a new budget. Only verified completed results are reused; unfinished tasks may have partial writes."
	return []FleetRecoveryPlan{view}, nil
}
