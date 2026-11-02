package serve

import (
	"context"
	"log/slog"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/team"
	"reasonix/internal/state/stats"
)

// reviewMaxTokens is the room a review's answer gets: a handful of changes,
// each a line or two of JSON.
const reviewMaxTokens = 2048

// memoryReviewer asks the title model, the cheap one a host keeps for short
// chores, with thinking off; nil when none is configured, and no review runs.
// Its spend is recorded as memory-review usage beside the title's.
func memoryReviewer() team.Reviewer {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	ref := strings.TrimSpace(cfg.Agent.TitleModel)
	if ref == "" {
		if start, ok := cfg.ResolveStartupChatModel(); ok {
			ref = start.Ref
		}
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok || !entry.Configured() {
		return nil
	}
	entry.Effort, entry.DefaultEffort, entry.SupportedEfforts, entry.Thinking = "", "", nil, "disabled"
	prov, err := boot.NewProviderWithProxy(entry, cfg.NetworkProxySpec())
	if err != nil {
		slog.Warn("serve: the memory review model could not start", "err", err)
		return nil
	}
	sink := stats.NewRecorder(event.Discard, config.StatsDir(), "serve")
	quote := boot.QuoteContextFor(cfg)
	modelRef := entry.Name + "/" + entry.Model
	return func(ctx context.Context, system, prompt string) (string, error) {
		ctx = provider.WithRequestAttemptCounter(ctx)
		var usage *provider.Usage
		defer func() {
			if usage = provider.UsageWithRequestAttemptCount(ctx, usage); usage != nil {
				e := event.Event{Kind: event.Usage, ModelRef: modelRef, Usage: usage, Pricing: entry.Price, UsageSource: event.UsageSourceMemoryReview}
				e.CostQuote = event.EnsureCostQuote(e, quote)
				sink.Emit(e)
			}
		}()
		ch, err := prov.Stream(ctx, provider.Request{
			Messages:    []provider.Message{{Role: provider.RoleSystem, Content: system}, {Role: provider.RoleUser, Content: prompt}},
			Temperature: provider.TemperaturePtr(0),
			MaxTokens:   reviewMaxTokens,
		})
		if err != nil {
			return "", err
		}
		var text strings.Builder
		for {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case chunk, ok := <-ch:
				if !ok {
					return text.String(), nil
				}
				switch chunk.Type {
				case provider.ChunkText:
					text.WriteString(chunk.Text)
				case provider.ChunkUsage:
					usage = chunk.Usage
				case provider.ChunkError:
					return "", chunk.Err
				}
			}
		}
	}
}
