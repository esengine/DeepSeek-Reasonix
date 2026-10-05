package delegation

import (
	"encoding/json"
	"strings"

	"reasonix/internal/contract/event"
)

// The delegation namespace. These ids are the whole discovery surface for the
// dispatchers: the provider schema hides them, so the catalog lists exactly
// this set and use_capability resolves exactly this set.

func (*TaskTool) CapabilityAliases() []string { return []string{"task:subagent"} }

func (*ReadOnlyTaskTool) CapabilityAliases() []string { return []string{"task:read_only_subagent"} }

func (*ParallelTasksTool) CapabilityAliases() []string { return []string{"task:parallel"} }

func (*FleetTool) CapabilityAliases() []string { return []string{"task:fleet"} }

// ResolveProfile reports how many sub-agents the batch dispatchers hand work
// to. Their items may each pick a different model, so only the count is shared;
// a reader that sees "12" is seeing twelve isolated contexts, not twelve steps.
func (*FleetTool) ResolveProfile(args json.RawMessage) *event.Profile {
	return batchDelegation("fleet", args)
}

func (*ParallelTasksTool) ResolveProfile(args json.RawMessage) *event.Profile {
	return batchDelegation("parallel_tasks", args)
}

func batchDelegation(name string, args json.RawMessage) *event.Profile {
	var p struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(args, &p); err != nil || len(p.Tasks) == 0 {
		return &event.Profile{Name: name}
	}
	return &event.Profile{Name: name, Count: len(p.Tasks)}
}

// ResolveProfile extracts model/effort from task args (and optional profile
// overrides) for dispatch-line display. Runtime execution re-resolves with the
// full precedence chain.
func (t *TaskTool) ResolveProfile(args json.RawMessage) *event.Profile {
	var p struct {
		Model   string `json:"model"`
		Effort  string `json:"effort"`
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil
	}
	profileModel, profileEffort := "", ""
	configModel, configEffort := "", ""
	if name := strings.TrimSpace(p.Profile); name != "" {
		if def, err := ResolveProfileDefinition(t.profileLookup, name); err == nil {
			profileModel, profileEffort = def.Model, def.Effort
		}
		if t.profileConfigModel != nil {
			configModel = t.profileConfigModel(name)
		}
		if t.profileConfigEffort != nil {
			configEffort = t.profileConfigEffort(name)
		}
	}
	model, effort := ResolveModelEffort(
		configModel, configEffort,
		p.Model, p.Effort,
		profileModel, profileEffort,
		t.subagentModel, t.subagentEffort,
	)
	name := strings.TrimSpace(p.Profile)
	if name == "" {
		name = "task"
	}
	return &event.Profile{Name: name, Count: 1, Model: model, Effort: effort}
}

func (r *ReadOnlyTaskTool) ResolveProfile(args json.RawMessage) *event.Profile {
	if r == nil || r.task == nil {
		return nil
	}
	return r.task.ResolveProfile(args)
}
