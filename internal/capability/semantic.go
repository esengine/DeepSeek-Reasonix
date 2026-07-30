package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const (
	semanticMaxCandidates = 12
	semanticMaxResults    = 3
	semanticTimeout       = 3 * time.Second
	semanticCacheTTL      = 5 * time.Minute
	semanticMaxTokens     = 256
)

// SemanticRouter calls a lightweight model when deterministic routing has no
// require/prefer hits. Failures fall back immediately to deterministic results.
type SemanticRouter struct {
	Provider provider.Provider
	Sink     event.Sink
	Model    string
	Effort   string
	// Pricing prices the router's own usage events; without it the routing
	// cost always displays as zero.
	Pricing *provider.Pricing
	// Audit receives router token/cost/latency counters (RecordRouterUsage).
	Audit *Audit

	mu    sync.Mutex
	cache map[string]semanticCacheEntry
}

type semanticCacheEntry struct {
	ids       []string
	expiresAt time.Time
}

// RouteSemantic may append up to 3 suggest candidates. It never overrides an
// existing require/prefer decision. On any failure it returns decision unchanged.
func (r *SemanticRouter) RouteSemantic(ctx context.Context, input string, catalog Catalog, decision RouteDecision) RouteDecision {
	if r == nil || r.Provider == nil {
		return decision
	}
	if hasStrongMatch(decision) {
		return decision
	}
	input = normalize(input)
	if input == "" {
		return decision
	}
	candidates := semanticPool(input, catalog.Entries)
	if len(candidates) == 0 {
		return decision
	}

	cacheKey := input + "|" + catalog.Fingerprint
	if ids, ok := r.cacheGet(cacheKey); ok {
		return mergeSemanticIDs(decision, catalog, ids, "semantic cache hit")
	}

	ids, err := r.callModel(ctx, input, candidates)
	if err != nil || len(ids) == 0 {
		return decision
	}
	r.cachePut(cacheKey, ids)
	return mergeSemanticIDs(decision, catalog, ids, "lightweight semantic match")
}

func hasStrongMatch(d RouteDecision) bool {
	for _, c := range d.Candidates {
		if c.Policy == AutoUseRequire || c.Policy == AutoUsePrefer {
			return true
		}
	}
	return false
}

func semanticPool(text string, entries []Entry) []Entry {
	var scored []Entry
	crossLanguageFallback := containsHan(text)
	for _, e := range entries {
		if e.Status == StatusDisabled || e.Status == StatusFailed {
			continue
		}
		if e.Kind != KindSkill && e.Kind != KindMCPTool && e.Kind != KindMCPServer {
			continue
		}
		if e.AutoUse == AutoUseOff {
			continue
		}
		if negativeMatch(text, e.NegativeTriggers) {
			continue
		}
		blob := normalize(e.Name + " " + e.Description + " " + strings.Join(e.Triggers, " "))
		if blob == "" {
			continue
		}
		// Prefer a cheap lexical match. For Han-script tasks, also admit the
		// bounded built-in/high-policy Skill set so English metadata does not make
		// the semantic router blind to Chinese requests.
		matched := false
		for _, tok := range strings.Fields(text) {
			if len(tok) < 3 {
				continue
			}
			if strings.Contains(blob, tok) {
				matched = true
				break
			}
		}
		if !matched && !(crossLanguageFallback && e.Kind == KindSkill && (e.Source == "builtin" || e.AutoUse == AutoUsePrefer || e.AutoUse == AutoUseRequire)) {
			continue
		}
		scored = append(scored, e)
	}
	if len(scored) > semanticMaxCandidates {
		scored = scored[:semanticMaxCandidates]
	}
	return scored
}

func containsHan(text string) bool {
	for _, r := range text {
		if r >= '\u3400' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func (r *SemanticRouter) callModel(ctx context.Context, input string, candidates []Entry) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, semanticTimeout)
	defer cancel()

	var b strings.Builder
	b.WriteString("Select up to 3 capability IDs relevant to the user task. ")
	b.WriteString("Reply with ONLY a JSON array of strings, e.g. [\"skill:review\"]. ")
	b.WriteString("If none fit, reply [].\n\nTask:\n")
	b.WriteString(input)
	b.WriteString("\n\nCandidates:\n")
	for _, e := range candidates {
		fmt.Fprintf(&b, "- %s (%s): %s\n", e.ID, e.Kind, truncate(e.Description, 120))
	}

	req := provider.Request{
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: b.String(),
		}},
		Temperature: provider.TemperaturePtr(0),
		MaxTokens:   semanticMaxTokens,
	}
	if r.Model != "" {
		// Model override is provider-specific; many providers ignore Request.Model
		// and use the bound model. The Host wires a dedicated provider when configured.
		_ = r.Model
	}

	start := time.Now()
	ch, err := r.Provider.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	var text strings.Builder
	var usage *provider.Usage
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkUsage:
			if chunk.Usage != nil {
				u := *chunk.Usage
				usage = &u
			}
		case provider.ChunkError:
			if chunk.Err != nil {
				return nil, chunk.Err
			}
		}
	}
	if usage != nil {
		if r.Audit != nil {
			r.Audit.RecordRouterUsage(usage.PromptTokens, usage.CompletionTokens, r.Pricing.Cost(usage), time.Since(start).Milliseconds())
		}
		if r.Sink != nil {
			r.Sink.Emit(event.Event{
				Kind:        event.Usage,
				Usage:       usage,
				Pricing:     r.Pricing,
				UsageSource: event.UsageSourceCapabilityRouter,
			})
		}
	}
	return parseSemanticIDs(text.String())
}

func parseSemanticIDs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty semantic response")
	}
	// Strip optional markdown fences.
	if i := strings.Index(raw, "["); i >= 0 {
		if j := strings.LastIndex(raw, "]"); j > i {
			raw = raw[i : j+1]
		}
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("invalid semantic JSON: %w", err)
	}
	out := make([]string, 0, semanticMaxResults)
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= semanticMaxResults {
			break
		}
	}
	return out, nil
}

func mergeSemanticIDs(decision RouteDecision, catalog Catalog, ids []string, reason string) RouteDecision {
	have := map[string]bool{}
	for _, c := range decision.Candidates {
		have[c.Entry.ID] = true
	}
	for _, id := range ids {
		if have[id] {
			continue
		}
		e, ok := catalog.Lookup(id)
		if !ok {
			continue
		}
		decision.Candidates = append(decision.Candidates, RouteCandidate{
			Entry:  e,
			Policy: AutoUseSuggest,
			Reason: reason,
		})
		have[id] = true
	}
	if len(decision.Candidates) > 5 {
		decision.Candidates = decision.Candidates[:5]
	}
	return decision
}

func (r *SemanticRouter) cacheGet(key string) ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		return nil, false
	}
	e, ok := r.cache[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return append([]string(nil), e.ids...), true
}

func (r *SemanticRouter) cachePut(key string, ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.cache = map[string]semanticCacheEntry{}
	}
	r.cache[key] = semanticCacheEntry{
		ids:       append([]string(nil), ids...),
		expiresAt: time.Now().Add(semanticCacheTTL),
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
