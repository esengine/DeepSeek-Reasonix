package resource

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type ResourceBudget struct {
	MaxTokens      int `json:"max_tokens"`
	MaxToolCalls   int `json:"max_tool_calls"`
	MaxMemoryNodes int `json:"max_memory_nodes"`
}

type Usage struct {
	Tokens      int `json:"tokens"`
	ToolCalls   int `json:"tool_calls"`
	MemoryNodes int `json:"memory_nodes"`
}

type Decision struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons,omitempty"`
	Budget  ResourceBudget
	Usage   Usage
}

type Reservation struct {
	ID        string         `json:"id,omitempty"`
	Allowed   bool           `json:"allowed"`
	Reasons   []string       `json:"reasons,omitempty"`
	Budget    ResourceBudget `json:"budget"`
	Reserved  Usage          `json:"reserved"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	ExpiresAt time.Time      `json:"expires_at,omitempty"`
}

type Coordinator struct {
	mu     sync.Mutex
	active map[string]Reservation
}

type CoordinatorSnapshot struct {
	ActiveReservations int   `json:"active_reservations"`
	Reserved           Usage `json:"reserved"`
}

func DefaultBudget() ResourceBudget {
	// These are advisory observability ceilings, not tight per-turn limits.
	// MaxMemoryNodes in particular must stay comfortably above the memory-graph
	// GC cap (memorycompiler.maxMemoryGraphNodes, currently 300) plus per-turn
	// writeback growth — otherwise a fully-populated graph permanently trips the
	// budget and the hardening verdict is stuck blocked forever.
	return ResourceBudget{
		MaxTokens:      200000,
		MaxToolCalls:   200,
		MaxMemoryNodes: 2000,
	}
}

func NewCoordinator() *Coordinator {
	return &Coordinator{active: map[string]Reservation{}}
}

func Enforce(budget ResourceBudget, usage Usage) Decision {
	budget = Normalize(budget)
	decision := Decision{Allowed: true, Budget: budget, Usage: usage}
	if usage.Tokens > budget.MaxTokens {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("token budget exceeded (%d>%d)", usage.Tokens, budget.MaxTokens))
	}
	if usage.ToolCalls > budget.MaxToolCalls {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("tool call budget exceeded (%d>%d)", usage.ToolCalls, budget.MaxToolCalls))
	}
	if usage.MemoryNodes > budget.MaxMemoryNodes {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("memory node budget exceeded (%d>%d)", usage.MemoryNodes, budget.MaxMemoryNodes))
	}
	return decision
}

func Reserve(budget ResourceBudget, usage Usage) Reservation {
	decision := Enforce(budget, usage)
	return Reservation{
		Allowed:  decision.Allowed,
		Reasons:  append([]string(nil), decision.Reasons...),
		Budget:   decision.Budget,
		Reserved: decision.Usage,
	}
}

func (c *Coordinator) Reserve(id string, budget ResourceBudget, usage Usage, now time.Time, ttl time.Duration) Reservation {
	if c == nil {
		reservation := Reserve(budget, usage)
		reservation.ID = id
		return reservation
	}
	budget = Normalize(budget)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = fmt.Sprintf("reservation-%d", now.UnixNano())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseExpiredLocked(now)
	if _, exists := c.active[id]; exists {
		return Reservation{
			ID:        id,
			Allowed:   false,
			Reasons:   []string{"reservation already active"},
			Budget:    budget,
			Reserved:  usage,
			CreatedAt: now.UTC(),
			ExpiresAt: now.Add(ttl).UTC(),
		}
	}
	projected := combineReservationUsage(c.reservedLocked(), usage)
	decision := Enforce(budget, projected)
	reservation := Reserve(budget, usage)
	reservation.ID = id
	reservation.CreatedAt = now.UTC()
	reservation.ExpiresAt = now.Add(ttl).UTC()
	if !decision.Allowed {
		reservation.Allowed = false
		for _, reason := range decision.Reasons {
			reservation.Reasons = append(reservation.Reasons, "global reservation "+reason)
		}
		reservation.Reasons = dedupeStrings(reservation.Reasons)
		return reservation
	}
	if reservation.Allowed {
		c.active[id] = reservation
	}
	return reservation
}

func (c *Coordinator) Commit(id string, actual Usage, now time.Time) Decision {
	id = strings.TrimSpace(id)
	if c == nil || id == "" {
		return Reservation{}.Commit(actual)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	reservation, ok := c.active[id]
	if !ok {
		return Decision{
			Allowed: false,
			Reasons: []string{
				"reservation not found or already committed",
			},
			Budget: Normalize(ResourceBudget{}),
			Usage:  actual,
		}
	}
	delete(c.active, id)
	decision := reservation.Commit(actual)
	if !reservation.ExpiresAt.IsZero() && now.After(reservation.ExpiresAt) {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, "reservation expired before commit")
	}
	decision.Reasons = dedupeStrings(decision.Reasons)
	return decision
}

func (c *Coordinator) Release(id string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.active, strings.TrimSpace(id))
}

func (c *Coordinator) Snapshot(now time.Time) CoordinatorSnapshot {
	if c == nil {
		return CoordinatorSnapshot{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseExpiredLocked(now)
	return CoordinatorSnapshot{
		ActiveReservations: len(c.active),
		Reserved:           c.reservedLocked(),
	}
}

func (r Reservation) Decision() Decision {
	return Decision{
		Allowed: r.Allowed,
		Reasons: append([]string(nil), r.Reasons...),
		Budget:  Normalize(r.Budget),
		Usage:   r.Reserved,
	}
}

func (r Reservation) Commit(actual Usage) Decision {
	budget := Normalize(r.Budget)
	decision := Enforce(budget, actual)
	if !r.Allowed {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, r.Reasons...)
	}
	if actual.Tokens > r.Reserved.Tokens {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("unreserved token usage (%d>%d)", actual.Tokens, r.Reserved.Tokens))
	}
	if actual.ToolCalls > r.Reserved.ToolCalls {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("unreserved tool call usage (%d>%d)", actual.ToolCalls, r.Reserved.ToolCalls))
	}
	if actual.MemoryNodes > r.Reserved.MemoryNodes {
		decision.Allowed = false
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("unreserved memory growth (%d>%d)", actual.MemoryNodes, r.Reserved.MemoryNodes))
	}
	decision.Reasons = dedupeStrings(decision.Reasons)
	return decision
}

func Normalize(budget ResourceBudget) ResourceBudget {
	def := DefaultBudget()
	if budget.MaxTokens <= 0 {
		budget.MaxTokens = def.MaxTokens
	}
	if budget.MaxToolCalls <= 0 {
		budget.MaxToolCalls = def.MaxToolCalls
	}
	if budget.MaxMemoryNodes <= 0 {
		budget.MaxMemoryNodes = def.MaxMemoryNodes
	}
	return budget
}

func ScaleForCanary(budget ResourceBudget, percent int) ResourceBudget {
	budget = Normalize(budget)
	if percent <= 0 {
		percent = 1
	}
	if percent > 100 {
		percent = 100
	}
	scale := func(v int) int {
		next := v * percent / 100
		if next < 1 {
			return 1
		}
		return next
	}
	return ResourceBudget{
		MaxTokens:      scale(budget.MaxTokens),
		MaxToolCalls:   scale(budget.MaxToolCalls),
		MaxMemoryNodes: budget.MaxMemoryNodes,
	}
}

func (c *Coordinator) releaseExpiredLocked(now time.Time) {
	for id, reservation := range c.active {
		if !reservation.ExpiresAt.IsZero() && now.After(reservation.ExpiresAt) {
			delete(c.active, id)
		}
	}
}

func (c *Coordinator) reservedLocked() Usage {
	total := Usage{}
	for _, reservation := range c.active {
		total = combineReservationUsage(total, reservation.Reserved)
	}
	return total
}

func combineReservationUsage(a, b Usage) Usage {
	memoryNodes := a.MemoryNodes
	if b.MemoryNodes > memoryNodes {
		memoryNodes = b.MemoryNodes
	}
	return Usage{
		Tokens:      a.Tokens + b.Tokens,
		ToolCalls:   a.ToolCalls + b.ToolCalls,
		MemoryNodes: memoryNodes,
	}
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
