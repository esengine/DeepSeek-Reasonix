package agent

import (
	"strings"

	"reasonix/internal/provider"
)

// HostContext supplies facts the host owes the request being built and keeps
// nowhere else: derived fresh, never stored, so nothing can fold them into
// history or resurrect a stale copy.
type HostContext interface {
	RequestContext() []string
}

// SetHostContext installs the source of host facts each request carries.
func (a *Agent) SetHostContext(h HostContext) { a.svc.hostContext = h }

// withHostContextTail appends what the host owes this request before the task
// identity: provenance about the run, then the ids a sign-off must cite. It is
// derived on every request, so nothing has to remember having shown it.
func (a *Agent) withHostContextTail(visible []provider.Message) []provider.Message {
	if a.svc.hostContext == nil {
		return visible
	}
	for _, block := range a.svc.hostContext.RequestContext() {
		if strings.TrimSpace(block) == "" {
			continue
		}
		visible = append(visible, provider.Message{Role: provider.RoleUser, Content: block})
	}
	return visible
}
