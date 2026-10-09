package agent

import (
	"fmt"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/safety/evidence"
)

// networkScopeRoots are the roots the host-side path bookkeeping (snapshots,
// baseline criteria, write reservations) compares against: the workspace only.
func (a *Agent) networkScopeRoots() []string {
	return []string{a.writeWorkspaceRoot}
}

// watchablePath reports whether a path may be handed to a filesystem call by
// host bookkeeping. A network path outside the workspace is decided by its
// spelling and never looked up.
func watchablePath(path, root string) bool {
	return fileutil.NetworkScope(path, []string{root}) == nil
}

// refuseNetworkToolPaths stops a call that names a network path outside the
// workspace before approval, preview, snapshot or criteria capture can look it
// up. The decision is the file tools' own, made once here for every tool whose
// paths the host can read from its arguments.
func (a *Agent) refuseNetworkToolPaths(plan *toolCallPlan) (toolOutcome, bool) {
	for _, args := range [][]byte{plan.permArgs, plan.execArgs, plan.evidenceArgs} {
		for _, p := range evidence.ToolCallPaths(args) {
			if err := fileutil.NetworkScope(p, a.networkScopeRoots()); err != nil {
				msg := fmt.Sprintf("refused: `%s` is a network path, a file on another machine, outside this workspace. It was refused by its spelling and never looked up.", p)
				return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg), refusalCode: fileutil.CodeNetworkPathOutsideScope}, true
			}
		}
	}
	return toolOutcome{}, false
}
