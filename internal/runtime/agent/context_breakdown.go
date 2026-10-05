package agent

import "reasonix/internal/contract/provider"

// ContextBreakdown says where the prompt's tokens went. "How full" is only
// actionable once you know what is filling it — a memory file, a tool
// catalogue and a run of huge outputs are fixed in completely different ways.
// Each figure uses the estimator the compaction thresholds use, so the parts
// describe the same prompt the gauge does.
type ContextBreakdown struct {
	System int `json:"system"` // system prompt: base instructions, memory, skills
	Tools  int `json:"tools"`  // tool schemas the provider is sent
	User   int `json:"user"`   // what the user typed
	Reply  int `json:"reply"`  // what the model said
	Output int `json:"output"` // what tools returned
	Total  int `json:"total"`
	Window int `json:"window"`
	// CompactAt is the lower of the two configured bounds, which is where this
	// session actually folds: a gauge drawn against the window instead can look
	// nearly empty at the moment maintenance happens.
	CompactAt int `json:"compactAt"`
	// Which bound that is, decided here rather than re-derived from the pair:
	// a reader told only the number cannot say why a large-window session folds,
	// and one comparing the two itself owns a second copy of the rule.
	Boundary string `json:"boundary"`
	// The window share, which is the bound the user configured and the one
	// they will look for when the other fires instead.
	CapacityAt int `json:"capacityAt"`
}

// ContextBreakdown measures the visible request one message class at a time.
func (a *Agent) ContextBreakdown() ContextBreakdown {
	if a == nil {
		return ContextBreakdown{}
	}
	visible := a.window().modelVisibleMessages()
	out := ContextBreakdown{
		Total:      a.ContextUsedTokens(),
		Window:     a.ContextWindow(),
		CompactAt:  a.window().compactTrigger(),
		Boundary:   a.window().compactBoundary(),
		CapacityAt: a.window().capacityCompactTrigger(),
		Tools:      a.window().classTokens(nil, true),
	}
	for _, class := range []struct {
		role provider.Role
		to   *int
	}{
		{provider.RoleSystem, &out.System},
		{provider.RoleUser, &out.User},
		{provider.RoleAssistant, &out.Reply},
		{provider.RoleTool, &out.Output},
	} {
		*class.to = a.window().classTokens(messagesWithRole(visible, class.role), false)
	}
	return out
}

// classTokens estimates one slice of the request. The schemas ride their own
// call because they are not messages at all, and on a small transcript they are
// the largest single item in the prompt.
func (a *contextWindow) classTokens(msgs []provider.Message, schemas bool) int {
	req := provider.Request{MaxTokens: a.maxOutputTokens, Temperature: provider.OptionalTemperature(a.temperature)}
	if len(msgs) > 0 {
		projected := a.providerProjectionMessages(provider.ModelMessages(msgs))
		for i := range projected {
			projected[i].CreatedAt = 0
		}
		req.Messages = projected
	}
	if schemas {
		req.Tools = a.estimationSurface()
	}
	if len(req.Messages) == 0 && len(req.Tools) == 0 {
		return 0
	}
	return a.estimatedRequestTokens(req)
}

func messagesWithRole(msgs []provider.Message, role provider.Role) []provider.Message {
	out := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == role {
			out = append(out, m)
		}
	}
	return out
}

// estimationSurface is the tool surface an estimate is measured against: what
// the last request actually carried, or the whole provider-visible set before
// one has gone out — which overstates rather than understates.
func (a *contextWindow) estimationSurface() []provider.ToolSchema {
	if a == nil {
		return nil
	}
	if sent := a.sess.lastProviderSchemas; len(sent) > 0 {
		return sent
	}
	if a.svc.tools == nil {
		return nil
	}
	return a.svc.tools.Schemas()
}
