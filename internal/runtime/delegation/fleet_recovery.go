package delegation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/execjournal"
	"strings"
)

type fleetRecoveryRequest struct {
	Action string `json:"action"`
	ID     string `json:"id"`
}

func (f *FleetTool) recoveryIdentity(ctx context.Context, spec ProfileExecSpec) (string, error) {
	if err := f.taskTool.settleSpecPrompts(ctx, &spec); err != nil {
		return "", err
	}
	spec.Worker.Model, spec.Worker.Effort = f.taskTool.childRuntimeRef(ctx, spec.Worker.Model, spec.Worker.Effort)
	spec.Worker.Model, spec.Worker.Effort = f.taskTool.effectiveIdentity(spec.Worker.Model, spec.Worker.Effort)
	spec.Worker.SystemPrompt = agent.ChildCompositionPrompt(ctx, spec.Worker.SystemPrompt)
	depth, err := f.taskTool.nextSubagentDepth(ctx)
	if err != nil {
		return "", err
	}
	reg, err := f.taskTool.childRegistry(&spec, depth, writeclaim.NewWriteGrant(spec.Grant.WritePaths), spec.Worker.Model)
	if err != nil {
		return "", err
	}
	_, schemas := toolIdentity(reg, childToolIdentityContext(ctx))
	data, err := json.Marshal(struct {
		Worker   WorkerSpec
		Grant    CapabilityGrant
		Context  ContextRequest
		Schemas  string
		MaxSteps int
	}{spec.Worker, spec.Grant, spec.Context, schemas, spec.Sched.MaxSteps})
	return bytesHash(data), err
}

func (f *FleetTool) saveRecovery(ctx context.Context, tasks []fleetTaskItem, call fleetCall, group string) (*fleetRecoveryLease, error) {
	if agent.SubagentDepth(ctx) > 0 || agent.ParentSession(ctx) == "" || f.taskTool.executionSessionPath(ctx) == "" {
		return nil, nil
	}
	root, err := f.recoveryRoot(ctx, true)
	if err != nil {
		return nil, err
	}
	id := "fr_" + rand.Text()
	release, err := filelock.TryAcquireRoot(root, id+".lock")
	if err != nil {
		root.Close()
		return nil, err
	}
	r := &fleetRecoveryLease{root: root, release: release, record: fleetRecoveryRecord{Version: 1, ID: id, Parent: agent.ParentSession(ctx), Workspace: f.taskTool.workspaceRoot, Tasks: tasks, FailFast: call.plan.failFast, Attempts: []string{group}, Identities: make([]string, len(tasks))}}
	r.record.BudgetScope = agent.SharedTaskBudgetScope(ctx)
	for i, spec := range call.specs {
		if tasks[i].AdoptRef != "" {
			r.record.Identities[i] = bytesHash([]byte(call.adopted[i].answer))
			continue
		}
		r.record.Identities[i], err = f.recoveryIdentity(ctx, spec)
		if err != nil {
			r.close()
			return nil, err
		}
	}
	if err = r.save(); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

func (f *FleetTool) recoveryGroup(ctx context.Context, group string) string {
	path := f.taskTool.executionSessionPath(ctx)
	if path == "" {
		return group
	}
	for _, entry := range execjournal.History(path) {
		if entry.Group == group {
			return group + "/attempt-" + rand.Text()
		}
	}
	return group
}

func (f *FleetTool) recoveryTasks(ctx context.Context, r fleetRecoveryRecord) ([]fleetTaskItem, error) {
	entries := map[string]execjournal.Entry{}
	for _, entry := range execjournal.History(f.taskTool.executionSessionPath(ctx)) {
		entries[entry.ID] = entry
	}
	tasks := append([]fleetTaskItem{}, r.Tasks...)
	for i := range tasks {
		if tasks[i].AdoptRef != "" {
			answer, err := f.adoptedAnswer(ctx, tasks[i].AdoptRef)
			if err != nil {
				return nil, err
			}
			if bytesHash([]byte(answer)) != r.Identities[i] {
				return nil, fmt.Errorf("adopted task %d changed since this fleet", i+1)
			}
		}
		for _, attempt := range r.Attempts {
			entry := entries[fleetNodeID(attempt, i)]
			if entry.Result != nil && entry.Result.State == "completed" {
				meta, err := f.taskTool.transcripts.LoadMeta(entry.Result.Ref)
				if err != nil {
					return nil, err
				}
				if meta.ExecutionID != entry.ID {
					return nil, fmt.Errorf("completed task %d changed since this fleet; create a new fleet with explicitly selected results", i+1)
				}
				tasks[i] = fleetTaskItem{ID: tasks[i].ID, DependsOn: tasks[i].DependsOn, Description: tasks[i].Description, AdoptRef: entry.Result.Ref}
			}
		}
		if tasks[i].AdoptRef != "" {
			if _, err := f.adoptedAnswer(ctx, tasks[i].AdoptRef); err != nil {
				return nil, err
			}
		}
	}
	return tasks, nil
}

func (f *FleetTool) executeRecovery(ctx context.Context, request fleetRecoveryRequest) (string, error) {
	if request.Action == "list" {
		return f.listRecovery(ctx)
	}
	if request.Action != "inspect" && request.Action != "resume" {
		return "", fmt.Errorf("recovery action must be list, inspect, or resume")
	}
	r, err := f.acquireRecovery(ctx, request.ID)
	if err != nil {
		return "", err
	}
	defer r.close()
	tasks, err := f.recoveryTasks(ctx, r.record)
	if err != nil {
		return "", err
	}
	if request.Action == "inspect" {
		data, err := json.Marshal(struct {
			ID     string          `json:"id"`
			Tasks  []fleetTaskItem `json:"tasks"`
			Notice string          `json:"notice"`
		}{r.record.ID, tasks, "Resume explicitly reruns unfinished tasks, which may already have partial writes. Completed results are reused; review their relevance before resuming."})
		return string(data), err
	}
	remaining := 0
	for _, item := range tasks {
		if item.AdoptRef == "" {
			remaining++
		}
	}
	if remaining == 0 {
		data, err := json.Marshal(tasks)
		return "Fleet is already complete; no tasks executed.\n" + string(data), err
	}
	call, err := f.preflight(ctx, tasks, r.record.FailFast)
	if err != nil {
		return "", err
	}
	for i, spec := range call.specs {
		if tasks[i].AdoptRef != "" {
			continue
		}
		identity, err := f.recoveryIdentity(ctx, spec)
		if err != nil {
			return "", err
		}
		if identity != r.record.Identities[i] {
			return "", fmt.Errorf("task %d recovery configuration changed; create a new fleet with the current profile, tools, write scope and context", i+1)
		}
	}
	if len(r.record.Attempts) >= 256 {
		return "", fmt.Errorf("fleet recovery attempt limit reached")
	}
	group, sink, _, _ := agent.CallContext(ctx)
	if sink == nil {
		sink = event.Discard
	}
	group = firstNonEmpty(group, "fleet") + "/recovery-" + rand.Text()
	r.record.Attempts = append(r.record.Attempts, group)
	if err = r.save(); err != nil {
		return "", err
	}
	ctx = agent.WithRawUserInput(ctx, firstNonEmpty(agent.RawUserInput(ctx, ""), "Explicit fleet recovery requested."))
	ctx, err = agent.RestoreSharedTaskBudget(ctx, f.taskTool.executionSessionPath(ctx), r.record.BudgetScope)
	if err != nil {
		return "", err
	}
	return f.runFleet(ctx, sink, call.specs, call.plan, call.adopted, group)
}

func (f *FleetTool) listRecovery(ctx context.Context) (string, error) {
	root, err := f.recoveryRoot(ctx, false)
	if os.IsNotExist(err) {
		return "[]", nil
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return "", err
	}
	type summary struct {
		ID    string `json:"id"`
		Tasks int    `json:"tasks"`
	}
	rows := []summary{}
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok {
			continue
		}
		r, err := readFleetRecovery(root, id)
		if err != nil || r.Parent != agent.ParentSession(ctx) || r.Workspace != f.taskTool.workspaceRoot {
			continue
		}
		rows = append(rows, summary{r.ID, len(r.Tasks)})
	}
	data, err := json.Marshal(rows)
	return string(data), err
}
