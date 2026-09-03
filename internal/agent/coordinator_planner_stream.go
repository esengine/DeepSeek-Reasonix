package agent

import (
	"context"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontext"
)

// planFromStream is the tool-less planner path: with no submit_plan available
// its result is always prose, which the host reads with the text fallback.
func (c *Coordinator) planFromStream(ctx context.Context, input string) (string, error) {
	// On failure, roll the just-added user message back: a dangling user turn
	// would produce consecutive user roles on the next plan. The executor fallback
	// keeps the turn alive, so the planner session must stay coherent.
	before := c.plannerSess.Snapshot()
	observed := c.appendPlannerTurnContext(ctx, before)
	rawInput := RawUserInput(ctx, input)
	rawContent := ""
	if input != rawInput {
		rawContent = rawInput
	}
	c.plannerSess.Add(provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: input, RawContent: rawContent})
	prefixShape := captureTurnContextShape(c.plannerSystem, nil, c.plannerSess.RewriteVersion(), c.plannerSess.Snapshot())
	previousShape := c.plannerLastPrefix
	if !c.plannerHasPrefix {
		previousShape = prefixShape
	}
	ctx = provider.WithRequestAttemptCounter(ctx)
	var usage *provider.Usage
	streamCompleted := false
	defer func() {
		accounted := provider.UsageWithRequestAttemptCount(ctx, usage)
		if accounted != nil || streamCompleted {
			diagnostics := CompareShape(previousShape, prefixShape, accounted, nil)
			diagnostics.SessionContext = eventSessionContextDiagnostics(observed)
			c.sink.Emit(event.Event{Kind: event.Usage, ModelRef: c.plannerModelRef, Usage: accounted, Pricing: c.plannerPricing, CacheDiagnostics: &diagnostics, Source: event.UsageSourcePlanner, UsageSource: event.UsageSourcePlanner})
		}
	}()

	planCtx, planCancel := context.WithCancel(ctx)
	defer planCancel()
	defer trackPublishedHostStream(planCtx, planCancel)()
	ch, err := c.planner.Stream(planCtx, provider.Request{
		Messages:    provider.ModelMessages(c.plannerSess.Messages),
		Temperature: provider.OptionalTemperature(c.temperature),
	})
	if err != nil {
		c.plannerSess.Replace(before)
		return "", err
	}

	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			c.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text, Source: event.UsageSourcePlanner})
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			c.plannerSess.Replace(before)
			return "", chunk.Err
		}
	}
	streamCompleted = true
	c.plannerLastPrefix = prefixShape
	c.plannerHasPrefix = true
	plan := text.String()
	c.plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: plan})
	return plan, nil
}

func (c *Coordinator) appendPlannerTurnContext(ctx context.Context, before []provider.Message) turnContextDiagnostics {
	snapshot, bootstrapOnly, _ := turnContextFromContext(ctx)
	if snapshot.Content == "" {
		return turnContextDiagnostics{}
	}
	previous, found := latestTurnContextSnapshot(before)
	appended := appendTurnContextToSession(c.plannerSess, before, snapshot, bootstrapOnly)
	observed := turnContextDiagnostics{
		snapshot: snapshot,
		stats:    sessioncontext.SectionDiagnostics(snapshot),
		target:   turnContextPlanner.String(),
	}
	if !appended {
		return observed
	}
	switch {
	case found:
		observed.reasons = changedTurnContextReasons(previous, snapshot)
	case bootstrapOnly || hasPriorConversation(before):
		observed.reasons = []string{"legacy_resume"}
	default:
		observed.reasons = []string{"first_seen"}
	}
	return observed
}
