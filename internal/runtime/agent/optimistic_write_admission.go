package agent

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
)

// reserveToolWrite admits the actual execution target, including a child proxy's
// resolved file writer. Explicit child fences keep their existing wrapper owner;
// legacy opaque effects keep their existing policy until effect coordination.
func (a *Agent) reserveToolWrite(ctx context.Context, writer tool.Tool, args json.RawMessage, readOnly bool, declared []string) (func(), error) {
	if a == nil || a.subagentDepth <= 0 {
		return a.reserveParentWrite(writer, args, readOnly, declared)
	}
	noop := func() {}
	if readOnly || writer == nil || a.svc.writeScheduler == nil {
		return noop, nil
	}
	grant := writeclaim.SubagentWriteGrant(ctx)
	if grant == nil || !grant.Declared().Optimistic {
		return noop, nil
	}
	if _, bound := writer.(pathBoundWriter); bound {
		return noop, nil // direct legacy callers may have installed their own guard
	}
	paths := declared
	if len(paths) == 0 {
		resolver, ok := writer.(tool.WritePathResolver)
		if !ok || !tool.WritesNamedPaths(writer) {
			return noop, nil
		}
		var err error
		paths, err = resolver.WritePaths(args)
		if err != nil {
			return noop, err
		}
	}
	if len(paths) == 0 {
		return noop, nil
	}
	claim, err := parentResolvedWriteReservation(grant.Declared().WorkspaceRoot, paths)
	if err != nil {
		return noop, err
	}
	return a.svc.writeScheduler.ReserveWrite(claim)
}
