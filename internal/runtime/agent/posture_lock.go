package agent

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension/dispatch"
)

// CodePostureToolNotAllowed identifies a call refused because the run's
// posture offers no such tool.
const CodePostureToolNotAllowed = "posture.tool_not_allowed"

// LockPosture fixes the gate and asker installed now: later SetGate and
// SetAsker calls change nothing, and a call to a tool the registry does not
// hold is refused with CodePostureToolNotAllowed. It cannot be undone.
func (a *Agent) LockPosture() { a.svc.postureLocked.Store(true) }

func (s *agentServices) setGate(g Gate) {
	if !s.postureLocked.Load() {
		s.gate = g
	}
}

func (s *agentServices) setAsker(as Asker) {
	if !s.postureLocked.Load() {
		s.asker = as
	}
}

// LockSurface fixes the tool registry and the extension dispatcher installed
// now. The host calls it once assembly has installed both; nothing that holds
// the agent can then replace either. It cannot be undone.
func (a *Agent) LockSurface() { a.svc.surfaceLocked.Store(true) }

func (s *agentServices) setTools(t *tool.Registry) {
	if !s.surfaceLocked.Load() {
		s.tools = t
	}
}

func (s *agentServices) setExtensions(d *dispatch.Dispatcher) {
	if !s.surfaceLocked.Load() {
		s.extensions = d
	}
}
