package agent

import (
	"context"
	"strings"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

const (
	// A classification is one word, but a thinking model spends its budget on
	// reasoning first and answers nothing at all under a tight cap — so the call
	// asks for thinking off and still leaves room for a short reply.
	commandClassEffort       = "disabled"
	commandClassMaxOutTokens = 64
	commandClassTimeout      = 20 * time.Second
)

// A classification is a lookup, not a sample: the same command answering
// differently on a retry is exactly what the cache exists to hide, and hiding it
// only fixes the second call. The turn's own temperature has nothing to say
// about it, and OptionalTemperature drops a zero, so the pointer is taken here.
var classTemperature = func() *float64 { zero := 0.0; return &zero }()

// triageProvider is the model configured for host classifications, and nothing
// else. Borrowing the turn's own provider would put extra calls into a stream
// the run is mid-conversation on — and would bill the main tier for a two-word
// answer — so an unconfigured triage model simply leaves the static verdict
// standing.
func (a *Agent) triageProvider() provider.Provider {
	if a == nil {
		return nil
	}
	return a.svc.triage
}

// askClass runs one classification and reports whether the reply is exactly the
// affirmative word.
func (a *Agent) askClass(ctx context.Context, systemPrompt, segment, affirmative string) bool {
	reply, ok := a.askTriage(ctx, systemPrompt, segment, event.UsageSourceClassifier)
	return ok && parseClassVerdict(reply, affirmative)
}

// askTriage runs one bounded request on the triage model and returns its reply;
// ok is false when the request or the stream failed. Usage is billed to source.
func (a *Agent) askTriage(ctx context.Context, systemPrompt, input, source string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, commandClassTimeout)
	defer cancel()

	var usage *provider.Usage
	defer func() {
		observeRootTokenUsage(ctx, usage)
		if usage != nil && usage.TotalTokens > 0 && a.svc.sink != nil {
			// Reported as what it is, on the model that answered it. Billing a
			// classification to compaction at the turn's own price hid both how
			// often the tables run short and how little the cheap tier costs.
			modelRef, pricing := a.modelRef, a.svc.pricing
			if ref := strings.TrimSpace(a.svc.triageRef); ref != "" {
				modelRef, pricing = ref, a.svc.triagePricing
			}
			a.svc.sink.Emit(event.Event{Kind: event.Usage, ModelRef: modelRef, Usage: usage,
				Pricing: pricing, UsageSource: source})
		}
	}()

	ch, err := a.triageProvider().Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: systemPrompt},
			{Role: provider.RoleUser, Content: input},
		},
		MaxTokens:      commandClassMaxOutTokens,
		EffortOverride: commandClassEffort,
		Temperature:    classTemperature,
	})
	if err != nil {
		return "", false
	}
	var reply strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			reply.WriteString(chunk.Text)
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			return "", false
		}
	}
	return reply.String(), true
}

// parseClassVerdict reads the verdict defensively: only the bare affirmative
// counts, so a hedged, chatty, or truncated reply lands on the safe side.
func parseClassVerdict(reply, affirmative string) bool {
	fields := strings.Fields(strings.ToUpper(reply))
	return len(fields) == 1 && fields[0] == strings.ToUpper(affirmative)
}
