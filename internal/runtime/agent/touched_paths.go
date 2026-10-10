package agent

import (
	"encoding/json"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// PathObserver is told which files a session's completed tool calls named.
// Reset forgets them all, for a conversation swapped out from under it.
type PathObserver interface {
	Observe(path string) error
	Reset()
}

// SetPathObserver installs the observer and seeds it from the conversation the
// agent already holds. Nil stops observing.
func (a *Agent) SetPathObserver(o PathObserver) {
	a.svc.pathObserver = o
	a.replayTouchedPaths(a.Session())
}

// touchedPaths is what a call names through the capabilities its tool declares:
// the one path it reads, and the paths it writes. A tool that cannot say, or
// whose resolution fails, names nothing — arguments are never read by key.
func touchedPaths(t tool.Tool, args json.RawMessage) []string {
	var out []string
	if rt, ok := t.(tool.ReadTargeter); ok {
		if p := rt.ReadTarget(args); p != "" {
			out = append(out, p)
		}
	}
	if wr, ok := t.(tool.WritePathResolver); ok {
		if paths, err := wr.WritePaths(args); err == nil {
			out = append(out, paths...)
		}
	}
	return out
}

// observeTouchedPaths feeds the arguments the call executed with. A path outside
// the workspace is refused by the observer and is not the session's to record.
func (a *Agent) observeTouchedPaths(t tool.Tool, args json.RawMessage) {
	if a.svc.pathObserver == nil {
		return
	}
	for _, p := range touchedPaths(t, args) {
		_ = a.svc.pathObserver.Observe(p)
	}
}

// replayTouchedPaths rebuilds the observer's set from a conversation: every
// call that has a result the host did not record as a failure. The transcript
// keeps the arguments the model wrote, so a call an extension rewrote replays
// under the original; paths resolve against the disk as it is now.
func (a *Agent) replayTouchedPaths(s *sessionstore.Session) {
	o := a.svc.pathObserver
	if o == nil {
		return
	}
	o.Reset()
	if s == nil {
		return
	}
	calls := map[string]provider.ToolCall{}
	for _, m := range s.Snapshot() {
		switch m.Role {
		case provider.RoleAssistant:
			for _, c := range m.ToolCalls {
				calls[c.ID] = c
			}
		case provider.RoleTool:
			c, ok := calls[m.ToolCallID]
			if !ok || m.ToolFailure != nil || a.svc.tools == nil {
				continue
			}
			a.replayCall(c)
		}
	}
}

// replayCall observes one stored call. A call through a stable proxy replays as
// the target the host recorded for it, with the arguments the proxy forwards.
func (a *Agent) replayCall(c provider.ToolCall) {
	t, ok := a.svc.tools.Get(c.Name)
	if !ok {
		return
	}
	args := json.RawMessage(c.Arguments)
	if reader, proxy := t.(tool.TargetArgsReader); proxy {
		target, found := a.svc.tools.Get(c.ResolvedName)
		if c.ResolvedName == "" || !found {
			return
		}
		t, args = target, reader.TargetArgs(args)
	}
	a.observeTouchedPaths(t, args)
}
