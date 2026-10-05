package main

import (
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/contract/event"
)

// conversationSources carry the session's tools and history. Every other
// source is a single-purpose auxiliary call, so one added later counts as
// auxiliary instead of dropping out of the split unnoticed.
var conversationSources = map[string]bool{
	event.UsageSourceExecutor: true,
	event.UsageSourcePlanner:  true,
	event.UsageSourceSubagent: true,
}

// spendSplit totals one side of the conversation/auxiliary divide.
type spendSplit struct {
	calls, tokens int
	cost          float64
}

func (s *spendSplit) add(u sourceUsage) {
	s.calls += u.Calls
	s.tokens += u.PromptTokens + u.CompletionTokens
	s.cost += u.Cost
}

// renderAuxiliarySpend prices the single-purpose calls against the
// conversation they surround — the share a separate endpoint could carry
// without touching the session's prompt cache.
func renderAuxiliarySpend(bySource map[string]sourceUsage, currency string) string {
	var conv, aux spendSplit
	names := make([]string, 0, len(bySource))
	for source, usage := range bySource {
		if usage.Calls == 0 {
			continue
		}
		if conversationSources[source] {
			conv.add(usage)
			continue
		}
		aux.add(usage)
		names = append(names, source)
	}
	total := spendSplit{conv.calls + aux.calls, conv.tokens + aux.tokens, conv.cost + aux.cost}
	if total.calls == 0 {
		return ""
	}
	line := fmt.Sprintf("**Auxiliary spend:** **%s of calls** (%s/%s) · **%s of tokens** (%s/%s)",
		pct(aux.calls, total.calls), comma(aux.calls), comma(total.calls),
		pct(aux.tokens, total.tokens), comma(aux.tokens), comma(total.tokens))
	if total.cost > 0 {
		line += fmt.Sprintf(" · **%.0f%% of cost** (%s%.4f/%s%.4f)",
			100*aux.cost/total.cost, currencySym(currency), aux.cost, currencySym(currency), total.cost)
	}
	if breakdown := auxiliaryBreakdown(bySource, names, currency); breakdown != "" {
		line += "\n\n**Auxiliary breakdown:** " + breakdown
	}
	return line + "\n\n"
}

// auxiliaryBreakdown itemizes the auxiliary sources, largest first. Tokens
// order the list because cost is absent whenever no rate card was configured.
func auxiliaryBreakdown(bySource map[string]sourceUsage, names []string, currency string) string {
	tokens := func(name string) int {
		u := bySource[name]
		return u.PromptTokens + u.CompletionTokens
	}
	sort.Slice(names, func(i, j int) bool {
		if tokens(names[i]) != tokens(names[j]) {
			return tokens(names[i]) > tokens(names[j])
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		u := bySource[name]
		part := fmt.Sprintf("%s ×%s (%s tok", name, comma(u.Calls), comma(tokens(name)))
		if u.Cost > 0 {
			part += fmt.Sprintf(", %s%.4f", currencySym(currency), u.Cost)
		}
		parts = append(parts, part+")")
	}
	return strings.Join(parts, " · ")
}
