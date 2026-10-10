package agent

import (
	"encoding/json"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// delegationProfile reports the sub-agents a call dispatches. A nil profile is
// what says the call kept the work in this context.
func delegationProfile(t tool.Tool, args json.RawMessage) *event.Profile {
	pr, ok := t.(interface {
		ResolveProfile(json.RawMessage) *event.Profile
	})
	if !ok {
		return nil
	}
	return pr.ResolveProfile(args)
}

func (a *Agent) emitRefreshedDispatch(c provider.ToolCall, profile *event.Profile) {
	a.svc.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID:           c.ID,
		Name:         c.Name,
		Args:         c.Arguments,
		ResolvedName: c.ResolvedName,
		CapabilityID: c.CapabilityID,
		ReadOnly:     *c.ResolvedReadOnly,
		Refreshed:    true,
		Issuer:       event.IssuedByModel,
		Profile:      profile,
		FileDiff: event.FileDiff{
			Diff: c.Diff, Added: c.Added, Removed: c.Removed,
		},
	}})
}

// announceDelegation marks a proxied call as a delegation while it runs; the
// batch's own refresh only lands once the sub-agent has already finished.
func (a *Agent) announceDelegation(plan *toolCallPlan) {
	if plan.resolvedMeta == nil {
		return
	}
	profile := delegationProfile(plan.resolvedMeta.Target, plan.resolvedMeta.Args)
	if profile == nil {
		return
	}
	c := plan.call
	c.ResolvedName = plan.resolvedMeta.TargetName
	c.CapabilityID = plan.resolvedMeta.CapabilityID
	readOnly := plan.resolvedMeta.ReadOnly
	c.ResolvedReadOnly = &readOnly
	a.emitRefreshedDispatch(c, profile)
}
