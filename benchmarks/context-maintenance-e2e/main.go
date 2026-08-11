// Cost-capped seed → resume → continue smoke for content-driven maintenance.
// Offline covers seed/resume; live continue needs DEEPSEEK_API_KEY and allows
// at most one summary after growing past compact_ratio=0.85.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	_ "reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

const (
	model         = "deepseek-v4-flash"
	baseURL       = "https://api.deepseek.com"
	windowTokens  = 1_000_000
	compactRatio  = 0.85
	defaultMaxUSD = 0.50
	fatBytes      = 32 * 1024
)

type costCap struct {
	maxUSD float64
	spent  float64
}

func (c *costCap) add(u *provider.Usage, pricing *provider.Pricing) error {
	if c == nil || u == nil || pricing == nil {
		return nil
	}
	c.spent += pricing.Cost(u)
	if c.maxUSD > 0 && c.spent > c.maxUSD {
		return fmt.Errorf("cost cap exceeded: spent $%.4f > max $%.4f", c.spent, c.maxUSD)
	}
	return nil
}

type meta struct {
	SeededAt          time.Time       `json:"seeded_at"`
	ProjectionVersion uint64          `json:"projection_version"`
	CanonicalTokens   int             `json:"canonical_tokens"`
	ProjectedTokens   int             `json:"projected_tokens"`
	TriggerTokens     int             `json:"trigger_tokens"`
	SummaryCalls      int             `json:"summary_calls"`
	SpentUSD          float64         `json:"spent_usd"`
	ContinueUsage     *provider.Usage `json:"continue_usage,omitempty"`
}

type recordSink struct {
	summaryStarts int
}

func (s *recordSink) Emit(e event.Event) {
	if e.Kind == event.CompactionStarted {
		s.summaryStarts++
	}
}

func prov() (provider.Provider, *provider.Pricing) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "DEEPSEEK_API_KEY not set (use -offline for seed/resume)")
		os.Exit(1)
	}
	p, err := provider.New("openai", provider.Config{
		Name: "e2e", BaseURL: baseURL, Model: model, APIKey: key,
		Extra: map[string]any{"max_output_tokens": 1024},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pricing := &provider.Pricing{Input: 0.14, Output: 0.28, CacheHit: 0.014, Currency: "$"}
	return p, pricing
}

func fatHistory(n int) []provider.Message {
	result := strings.Repeat("x", fatBytes)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "You are a terse coding agent."},
		{Role: provider.RoleUser, Content: "Review the large tool outputs carefully."},
	}
	for i := range n {
		id := fmt.Sprintf("tool-%d", i)
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: result},
		)
	}
	return append(msgs,
		provider.Message{Role: provider.RoleUser, Content: "summarize status when asked"},
		provider.Message{Role: provider.RoleAssistant, Content: "standing by"},
	)
}

func newAgent(p provider.Provider, sess *agent.Session, path string, sink event.Sink) *agent.Agent {
	return agent.New(p, tool.NewRegistry(), sess, agent.Options{
		ContextWindow: windowTokens,
		CompactRatio:  compactRatio,
		RecentKeep:    2,
		SessionPath:   path,
		WorkspaceID:   "cm-e2e",
		ModelRef:      "deepseek/" + model,
		MaxSteps:      2,
	}, sink)
}

func writeMeta(dir string, m meta) {
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readMeta(dir string) meta {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var m meta
	if err := json.Unmarshal(raw, &m); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return m
}

func seed(dir string, offline bool, cap *costCap) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	path := filepath.Join(dir, "session.jsonl")
	// ~80×32KB tool results: well under 85% of 1M for a cold estimate.
	sess := &agent.Session{Messages: fatHistory(80)}
	sink := &recordSink{}
	var p provider.Provider
	if !offline {
		p, _ = prov()
	}
	a := newAgent(p, sess, path, sink)
	before := a.ContextMaintenanceSnapshot()
	if before.TriggerTokens <= 0 {
		fmt.Fprintln(os.Stderr, "seed: missing trigger tokens")
		os.Exit(1)
	}
	if before.ProjectedTokens >= before.TriggerTokens {
		fmt.Fprintf(os.Stderr, "seed fixture already at/above trigger: projected=%d trigger=%d\n",
			before.ProjectedTokens, before.TriggerTokens)
		os.Exit(1)
	}
	if err := a.PrepareContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "seed prepare:", err)
		os.Exit(1)
	}
	after := a.ContextMaintenanceSnapshot()
	if after.ProjectionVersion != 0 {
		fmt.Fprintf(os.Stderr, "seed installed projection version %d below compact_ratio\n", after.ProjectionVersion)
		os.Exit(1)
	}
	if sink.summaryStarts != 0 {
		fmt.Fprintf(os.Stderr, "seed started %d summary calls below trigger\n", sink.summaryStarts)
		os.Exit(1)
	}
	if err := sess.Save(path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m := meta{
		SeededAt:          time.Now(),
		ProjectionVersion: after.ProjectionVersion,
		CanonicalTokens:   after.CanonicalTokens,
		ProjectedTokens:   after.ProjectedTokens,
		TriggerTokens:     after.TriggerTokens,
		SummaryCalls:      sink.summaryStarts,
		SpentUSD:          cap.spent,
	}
	writeMeta(dir, m)
	fmt.Printf("seed ok projected=%d trigger=%d version=%d summary_calls=%d spent=$%.4f\n",
		m.ProjectedTokens, m.TriggerTokens, m.ProjectionVersion, m.SummaryCalls, m.SpentUSD)
}

func resume(dir string, offline bool) {
	path := filepath.Join(dir, "session.jsonl")
	m := readMeta(dir)
	sess, err := agent.LoadSession(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sink := &recordSink{}
	var p provider.Provider
	if !offline {
		p, _ = prov()
	}
	a := newAgent(p, sess, path, sink)
	snap := a.ContextMaintenanceSnapshot()
	if snap.ProjectionVersion != m.ProjectionVersion {
		fmt.Fprintf(os.Stderr, "resume version = %d, want %d\n", snap.ProjectionVersion, m.ProjectionVersion)
		os.Exit(1)
	}
	if err := a.PrepareContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "resume prepare:", err)
		os.Exit(1)
	}
	after := a.ContextMaintenanceSnapshot()
	if after.ProjectionVersion != m.ProjectionVersion {
		fmt.Fprintf(os.Stderr, "resume advanced version to %d\n", after.ProjectionVersion)
		os.Exit(1)
	}
	if sink.summaryStarts != 0 {
		fmt.Fprintf(os.Stderr, "resume re-ran summary %d times\n", sink.summaryStarts)
		os.Exit(1)
	}
	fmt.Printf("resume ok idle=%s version=%d summary_calls=0\n",
		time.Since(m.SeededAt).Round(time.Second), m.ProjectionVersion)
}

func cont(dir string, offline bool, cap *costCap) {
	if offline {
		fmt.Fprintln(os.Stderr, "continue requires live provider (omit -offline)")
		os.Exit(1)
	}
	path := filepath.Join(dir, "session.jsonl")
	m := readMeta(dir)
	sess, err := agent.LoadSession(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Grow past the 85% trigger with additional fat tool turns (skip system/user prelude).
	for _, msg := range fatHistory(40)[2:] {
		sess.Add(msg)
	}
	sink := &recordSink{}
	p, pricing := prov()
	a := newAgent(p, sess, path, sink)
	before := a.ContextMaintenanceSnapshot()
	if before.ProjectedTokens < before.TriggerTokens {
		fmt.Fprintf(os.Stderr, "continue fixture still below trigger: projected=%d trigger=%d\n",
			before.ProjectedTokens, before.TriggerTokens)
		os.Exit(1)
	}
	if err := a.PrepareContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "continue prepare:", err)
		os.Exit(1)
	}
	if sink.summaryStarts > 1 {
		fmt.Fprintf(os.Stderr, "continue started %d summaries, want ≤1\n", sink.summaryStarts)
		os.Exit(1)
	}
	after := a.ContextMaintenanceSnapshot()
	if sink.summaryStarts == 1 && after.ProjectionVersion != m.ProjectionVersion+1 {
		fmt.Fprintf(os.Stderr, "continue version = %d, want %d after one summary\n",
			after.ProjectionVersion, m.ProjectionVersion+1)
		os.Exit(1)
	}
	if u := a.LastUsage(); u != nil {
		if err := cap.add(u, pricing); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		m.ContinueUsage = u
	}
	startsBefore := sink.summaryStarts
	_ = a.Run(context.Background(), "Reply with exactly: ok")
	if u := a.LastUsage(); u != nil {
		if err := cap.add(u, pricing); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if sink.summaryStarts != startsBefore {
		fmt.Fprintf(os.Stderr, "post-checkpoint run started extra summaries (%d→%d)\n",
			startsBefore, sink.summaryStarts)
		os.Exit(1)
	}
	if err := sess.Save(path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	final := a.ContextMaintenanceSnapshot()
	m.ProjectionVersion = final.ProjectionVersion
	m.ProjectedTokens = final.ProjectedTokens
	m.CanonicalTokens = final.CanonicalTokens
	m.SummaryCalls = sink.summaryStarts
	m.SpentUSD = cap.spent
	writeMeta(dir, m)
	fmt.Printf("continue ok version=%d projected=%d summary_calls=%d spent=$%.4f\n",
		m.ProjectionVersion, m.ProjectedTokens, m.SummaryCalls, m.SpentUSD)
}

func main() {
	dir := flag.String("dir", "benchmarks/context-maintenance-e2e/run", "state directory")
	maxUSD := flag.Float64("max-usd", defaultMaxUSD, "hard cost cap for live API legs")
	offline := flag.Bool("offline", false, "skip live provider (seed/resume only)")
	flag.Parse()
	cap := &costCap{maxUSD: *maxUSD}
	switch flag.Arg(0) {
	case "seed":
		seed(*dir, *offline, cap)
	case "resume":
		resume(*dir, *offline)
	case "continue":
		cont(*dir, *offline, cap)
	default:
		fmt.Fprintln(os.Stderr, "usage: context-maintenance-e2e [-dir DIR] [-max-usd N] [-offline] seed|resume|continue")
		os.Exit(1)
	}
}
