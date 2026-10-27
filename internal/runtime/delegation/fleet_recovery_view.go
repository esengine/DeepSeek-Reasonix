package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/execjournal"
	"reasonix/internal/state/sessionstore"
	"strings"
)

type FleetRecoveryTask struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	DependsOn   []string `json:"dependsOn"`
	State       string   `json:"state"`
	Ref         string   `json:"ref,omitempty"`
}

type FleetRecoveryPlan struct {
	Legacy    bool                `json:"legacy,omitempty"`
	Arguments json.RawMessage     `json:"-"`
	ID        string              `json:"id"`
	Snapshot  string              `json:"snapshot"`
	Tasks     []FleetRecoveryTask `json:"tasks"`
	Ready     bool                `json:"ready"`
	Reason    string              `json:"reason,omitempty"`
}

func (s *SubagentStore) FleetRecoveryPlans(path, parent, workspace, ref string) ([]FleetRecoveryPlan, error) {
	meta, err := s.LoadMeta(ref)
	if err != nil {
		return nil, err
	}
	if err := s.checkOwner(ref, meta, parent, workspace); err != nil {
		return nil, err
	}
	f := NewFleetTool(&TaskTool{transcripts: s, workspaceRoot: workspace})
	ctx := WithExecutionSessionPath(agent.WithParentSession(context.Background(), parent), path)
	root, err := f.recoveryRoot(ctx, false)
	if os.IsNotExist(err) {
		return s.legacyFleetPlan(path, parent, workspace, meta)
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	history := execjournal.History(path)
	rows := []FleetRecoveryPlan{}
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok {
			continue
		}
		r, err := readFleetRecovery(root, id)
		if err != nil {
			return nil, err
		}
		if r.Parent != parent || r.Workspace != workspace {
			continue
		}
		if !recoveryIncludes(r, meta, history) {
			continue
		}
		rows = append(rows, f.recoveryView(ctx, r))
	}
	if len(rows) == 0 {
		return s.legacyFleetPlan(path, parent, workspace, meta)
	}
	return rows, nil
}

func recoveryIncludes(r fleetRecoveryRecord, meta sessionstore.SubagentMeta, history []execjournal.Entry) bool {
	for _, attempt := range r.Attempts {
		for _, e := range history {
			if e.Group == attempt && (e.ID == meta.ExecutionID || (e.Result != nil && e.Result.Ref == meta.Ref)) {
				return true
			}
		}
	}
	return false
}

func (f *FleetTool) recoveryView(ctx context.Context, r fleetRecoveryRecord) FleetRecoveryPlan {
	view := FleetRecoveryPlan{ID: r.ID, Tasks: []FleetRecoveryTask{}}
	items, err := f.recoveryTasks(ctx, r)
	if err != nil {
		view.Reason = err.Error()
		items = r.Tasks
	}
	for i, item := range items {
		state := "unfinished"
		if item.AdoptRef != "" {
			state = "completed"
		} else {
			view.Ready = err == nil
		}
		view.Tasks = append(view.Tasks, FleetRecoveryTask{ID: firstNonEmpty(item.ID, fmt.Sprint(i+1)), Description: item.Description, Prompt: item.Prompt, DependsOn: append([]string{}, item.DependsOn...), State: state, Ref: item.AdoptRef})
	}
	data, _ := json.Marshal(struct {
		Record fleetRecoveryRecord
		Tasks  []FleetRecoveryTask
	}{r, view.Tasks})
	view.Snapshot = bytesHash(data)
	if lease, err := f.acquireRecovery(ctx, r.ID); err != nil {
		view.Ready = false
		view.Reason = "The fleet is running or its recovery record is unavailable."
	} else {
		lease.close()
	}
	return view
}
