// Package usageledger provides one fail-closed projection for billable model usage.
package usageledger

import (
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const USDScale int64 = 10_000_000_000

type Tokens struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type ModelUsage struct {
	Model         string `json:"model,omitempty"`
	Source        string `json:"source"`
	Usage         Tokens `json:"usage"`
	CostIsPartial bool   `json:"cost_is_partial"`
	CostUSDTicks  *int64 `json:"cost_usd_ticks,omitempty"`
}

type Projection struct {
	Usage                   Tokens                `json:"usage"`
	ModelUsage              map[string]ModelUsage `json:"modelUsage"`
	UsageIsIncomplete       bool                  `json:"usage_is_incomplete"`
	CostIsPartial           bool                  `json:"cost_is_partial"`
	TotalCostUSDTicks       *int64                `json:"total_cost_usd_ticks,omitempty"`
	TotalCostUSD            *float64              `json:"total_cost_usd,omitempty"`
	IncompleteReasons       []string              `json:"incomplete_reasons,omitempty"`
	OpenBackgroundSubagents int                   `json:"open_background_subagents,omitempty"`
}

type row struct {
	model, source string
	tokens        Tokens
	cost          *big.Rat
	partial       bool
}

type Ledger struct {
	mu                sync.Mutex
	tokens            Tokens
	rows              map[string]*row
	cost              *big.Rat
	costPartial       bool
	incompleteReasons map[string]struct{}
	openBackground    map[string]struct{}
	drainTimedOut     bool
}

func New() *Ledger {
	return &Ledger{rows: map[string]*row{}, cost: new(big.Rat), incompleteReasons: map[string]struct{}{}, openBackground: map[string]struct{}{}}
}

func (l *Ledger) Add(e event.Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.Kind == event.BackgroundJobLifecycle {
		l.addBackgroundJob(e.BackgroundJob)
		return
	}
	if e.Kind != event.Usage || e.Usage == nil {
		return
	}
	if l.rows == nil {
		l.rows = map[string]*row{}
	}
	if l.cost == nil {
		l.cost = new(big.Rat)
	}
	if l.incompleteReasons == nil {
		l.incompleteReasons = map[string]struct{}{}
	}
	if l.openBackground == nil {
		l.openBackground = map[string]struct{}{}
	}
	t := tokens(e.Usage)
	addTokens(&l.tokens, t)
	source := strings.TrimSpace(e.UsageSource)
	if source == "" {
		source = event.UsageSourceExecutor
	}
	model := strings.TrimSpace(e.UsageModel)
	key := source + ":" + model
	r := l.rows[key]
	if r == nil {
		r = &row{model: model, source: source, cost: new(big.Rat)}
		l.rows[key] = r
	}
	addTokens(&r.tokens, t)
	if model == "" {
		l.incompleteReasons["missing_model_attribution"] = struct{}{}
	}
	cost, ok := usdCost(e.Usage, e.Pricing)
	if !ok {
		l.costPartial = true
		r.partial = true
		return
	}
	l.cost.Add(l.cost, cost)
	r.cost.Add(r.cost, cost)
}

func (l *Ledger) addBackgroundJob(job event.BackgroundJob) {
	if strings.TrimSpace(job.Kind) != "task" || strings.TrimSpace(job.ID) == "" {
		return
	}
	if l.openBackground == nil {
		l.openBackground = map[string]struct{}{}
	}
	switch strings.TrimSpace(job.Status) {
	case "running":
		l.openBackground[job.ID] = struct{}{}
	case "done", "failed", "killed":
		delete(l.openBackground, job.ID)
	case "drain_timeout":
		l.openBackground[job.ID] = struct{}{}
		l.drainTimedOut = true
	}
}

func (l *Ledger) MarkIncomplete(reason string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.incompleteReasons == nil {
		l.incompleteReasons = map[string]struct{}{}
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		l.incompleteReasons[reason] = struct{}{}
	}
}

func (l *Ledger) Projection() Projection {
	if l == nil {
		return Projection{ModelUsage: map[string]ModelUsage{}}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	p := Projection{Usage: l.tokens, ModelUsage: make(map[string]ModelUsage, len(l.rows)), CostIsPartial: l.costPartial, OpenBackgroundSubagents: len(l.openBackground)}
	if len(l.rows) == 0 {
		p.CostIsPartial = true
		p.IncompleteReasons = append(p.IncompleteReasons, "usage_not_reported")
	}
	for key, r := range l.rows {
		mr := ModelUsage{Model: r.model, Source: r.source, Usage: r.tokens, CostIsPartial: r.partial}
		if !r.partial {
			ticks := ratTicks(r.cost)
			mr.CostUSDTicks = &ticks
		}
		p.ModelUsage[key] = mr
	}
	for reason := range l.incompleteReasons {
		p.IncompleteReasons = append(p.IncompleteReasons, reason)
	}
	if len(l.openBackground) > 0 {
		p.IncompleteReasons = append(p.IncompleteReasons, "background_subagent_usage_pending")
	}
	if l.drainTimedOut {
		p.IncompleteReasons = append(p.IncompleteReasons, "background_subagent_drain_timeout")
	}
	sort.Strings(p.IncompleteReasons)
	p.UsageIsIncomplete = len(p.IncompleteReasons) > 0
	if !p.CostIsPartial && len(l.rows) > 0 {
		ticks := ratTicks(l.cost)
		usd := float64(ticks) / float64(USDScale)
		p.TotalCostUSDTicks = &ticks
		p.TotalCostUSD = &usd
	}
	return p
}

func tokens(u *provider.Usage) Tokens {
	return Tokens{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, CacheReadInputTokens: u.CacheHitTokens, CacheCreationInputTokens: u.CacheMissTokens}
}

func addTokens(dst *Tokens, src Tokens) {
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadInputTokens += src.CacheReadInputTokens
	dst.CacheCreationInputTokens += src.CacheCreationInputTokens
}

func usdCost(u *provider.Usage, p *provider.Pricing) (*big.Rat, bool) {
	if u == nil || p == nil || !isUSD(p.Currency) {
		return nil, false
	}
	hit, miss := u.CacheHitTokens, u.CacheMissTokens
	if hit+miss == 0 && u.PromptTokens > 0 {
		miss = u.PromptTokens
	} else if miss == 0 && hit > 0 && u.PromptTokens > hit {
		miss = u.PromptTokens - hit
	}
	total := new(big.Rat)
	addRate := func(count int, rate float64) bool {
		r, ok := new(big.Rat).SetString(strconv.FormatFloat(rate, 'f', -1, 64))
		if !ok {
			return false
		}
		r.Mul(r, big.NewRat(int64(count), 1_000_000))
		total.Add(total, r)
		return true
	}
	if !addRate(hit, p.CacheHit) || !addRate(miss, p.Input) || !addRate(u.CompletionTokens, p.Output) {
		return nil, false
	}
	return total, true
}

func isUSD(currency string) bool {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "$", "USD", "US$":
		return true
	default:
		return false
	}
}

func ratTicks(cost *big.Rat) int64 {
	if cost == nil {
		return 0
	}
	scaled := new(big.Rat).Mul(cost, big.NewRat(USDScale, 1))
	n := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	rem := new(big.Int).Rem(scaled.Num(), scaled.Denom())
	if new(big.Int).Lsh(new(big.Int).Abs(rem), 1).Cmp(new(big.Int).Abs(scaled.Denom())) >= 0 {
		if scaled.Sign() >= 0 {
			n.Add(n, big.NewInt(1))
		} else {
			n.Sub(n, big.NewInt(1))
		}
	}
	return n.Int64()
}
