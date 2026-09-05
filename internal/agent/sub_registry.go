// sub_registry.go — the tool surface one delegated run may reach.
package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

// subRegistryFor narrows the parent's tools to what this run may touch: the
// profile ceiling intersected with the call, the read-only or writer registry
// for the grant, the write claim bound into the tools that must honour it, and
// the report tool a worker owing a typed verdict answers with.
func (t *TaskTool) subRegistryFor(spec *ProfileExecSpec, childDepth int) (*tool.Registry, error) {
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
		// Explicit paths are an execution boundary and rebind or drop tools that
		// cannot honour it. A synthesized whole-workspace claim is a scheduling
		// boundary, and preserves the session's existing tool boundaries.
		if !spec.Grant.WritePaths.Empty() && !spec.Grant.WritePaths.WholeWorkspace {
			keepBash := t.bashCanEnforceWriteRoots()
			bound, removed := BindWritePaths(subReg, spec.Grant.WritePaths, t.workspaceRoot, keepBash)
			subReg = bound
			if len(removed) > 0 && subReg.Len() == 0 {
				return nil, fmt.Errorf("no path-bound write tools available after dropping unbound writers: %s", strings.Join(removed, ", "))
			}
		}
	}
	// Never on the parent surface: the gate reads a child's report, and exposing
	// it upward would let a turn file a verdict on its own behalf.
	if spec.Worker.ReviewReport != "" {
		AttachReviewReportTool(subReg)
	}
	return subReg, nil
}
