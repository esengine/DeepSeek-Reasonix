// sub_registry.go — the tool surface one delegated run may reach.
package delegation

import (
	"fmt"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
)

// subRegistryFor narrows the parent's tools to what this run may touch: the
// profile ceiling intersected with the call, the read-only or writer registry
// for the grant, and the write claim bound into the tools that must honour it.
// The report tool is not here: it carries a permission, and a permission is
// issued once the execution it belongs to exists (see AttachReviewReport).
func (t *TaskTool) subRegistryFor(spec *ProfileExecSpec, childDepth int, grant *writeclaim.WriteGrant) (*tool.Registry, error) {
	toolNames, err := IntersectToolLists(t.parentReg, spec.Grant.ProfileTools, spec.Grant.CallTools)
	if err != nil {
		return nil, err
	}
	var subReg *tool.Registry
	if spec.Grant.ReadOnly {
		if subReg, err = t.readOnlySubRegistry(spec, toolNames, childDepth); err != nil {
			return nil, err
		}
	} else {
		subReg = t.buildSubReg(toolNames, childDepth)
	}
	return subReg, nil
}

// childRegistry is the narrowed parent registry plus what the run's own model
// brings. The model is the one the run really starts on, after any routing to a
// vision model, so the tools match what will read them.
func (t *TaskTool) childRegistry(spec *ProfileExecSpec, childDepth int, grant *writeclaim.WriteGrant, modelRef string) (*tool.Registry, error) {
	reg, err := t.subRegistryFor(spec, childDepth, grant)
	if err != nil {
		return nil, err
	}
	if t.childTools == nil || strings.TrimSpace(modelRef) == "" {
		return t.bindExecutionWriters(reg, spec, grant)
	}
	ceiling, err := IntersectToolLists(t.parentReg, spec.Grant.ProfileTools, spec.Grant.CallTools)
	if err != nil {
		return nil, err
	}
	for _, extra := range t.childTools(modelRef) {
		name := extra.Name()
		if _, held := reg.Get(name); held {
			continue
		}
		if len(ceiling) > 0 && !slices.Contains(ceiling, name) {
			continue
		}
		if spec.Grant.ReadOnly && !extra.ReadOnly() {
			continue
		}
		reg.Add(extra)
	}
	return t.bindExecutionWriters(reg, spec, grant)
}

func (t *TaskTool) bindExecutionWriters(reg *tool.Registry, spec *ProfileExecSpec, grant *writeclaim.WriteGrant) (*tool.Registry, error) {
	if !spec.Grant.ReadOnly && !spec.Grant.WritePaths.Empty() && !spec.Grant.WritePaths.WholeWorkspace && !spec.Grant.WritePaths.Optimistic {
		scheduler := t.scheduler
		if t.mutations != nil {
			scheduler = nil
		}
		bound, removed := agent.BindWritePaths(reg, grant, t.gate, scheduler, t.workspaceRoot, t.bashCanEnforceWriteRoots())
		if len(removed) > 0 && bound.Len() == 0 {
			return nil, fmt.Errorf("no path-bound write tools available after dropping unbound writers: %s", strings.Join(removed, ", "))
		}
		return bound, nil
	}
	return t.bindOptimisticWriters(reg, grant), nil
}

func (t *TaskTool) bindOptimisticWriters(reg *tool.Registry, grant *writeclaim.WriteGrant) *tool.Registry {
	if t.mutations != nil || !grant.Declared().Optimistic {
		return reg
	}
	return agent.BindOptimisticWriters(reg, grant, t.gate, t.scheduler, t.workspaceRoot)
}
