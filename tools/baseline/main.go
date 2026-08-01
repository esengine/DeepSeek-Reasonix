// Command baseline collects inference quality baselines from a configured
// Reasonix provider. It sends N prompts, measures latency / TTFT / output
// length / token usage, and writes a baseline JSON that model-watchdog can
// consume for anomaly detection.
//
// Usage:
//
//	go run ./tools/baseline -model deepseek/deepseek-chat -n 100 -out baseline.json
//	go run ./tools/baseline -model deepseek/deepseek-chat -n 100 -dry-run
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/provider"

	// Register all provider kinds via init().
	_ "reasonix/internal/provider/anthropic"
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/provider/responses"
)

// ---------- data types ----------

// Sample is one completed inference call.
type Sample struct {
	Index        int     `json:"index"`
	LatencyMs    float64 `json:"latency_ms"` // wall-clock total
	TTFTMs       float64 `json:"ttft_ms"`    // time to first text token
	OutputChars  int     `json:"output_chars"`
	OutputTokens int     `json:"output_tokens"`
	PromptTokens int     `json:"prompt_tokens"`
	ReasoningTok int     `json:"reasoning_tokens"`
	FinishReason string  `json:"finish_reason"`
	Error        string  `json:"error,omitempty"`
}

// Stats is a computed percentile summary.
type Stats struct {
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// Baseline is the output artifact consumed by model-watchdog.
type Baseline struct {
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	N           int    `json:"n"`
	Errors      int    `json:"errors"`
	CreatedAt   string `json:"created_at"`
	LatencyMs   Stats  `json:"latency_ms"`
	TTFTMs      Stats  `json:"ttft_ms"`
	OutputChars Stats  `json:"output_chars"`
	OutputTok   Stats  `json:"output_tokens"`
	PromptTok   Stats  `json:"prompt_tokens"`
	// Derived thresholds for watchdog (mean ± 3σ, p99 × 5, etc.)
	Thresholds map[string]float64 `json:"thresholds"`
	Samples    []Sample           `json:"samples,omitempty"` // only with -verbose
}

// ---------- prompts ----------

// prompts is a diverse set covering short/long, Chinese/English, code/prose.
var prompts = []string{
	"用一句话解释什么是量子纠缠。",
	"Write a haiku about debugging at 3am.",
	"解释 Go 语言中 channel 和 mutex 的区别，各举一个适用场景。",
	"What is the time complexity of merge sort and why?",
	"写一个 Python 函数，判断一个字符串是否是回文。",
	"Summarize the key ideas of TCP congestion control in 3 bullet points.",
	"用通俗语言解释 CAP 定理。",
	"Explain the difference between stack and heap memory.",
	"写一首关于代码审查的五言绝句。",
	"What are the SOLID principles? List them with one-line descriptions.",
}

// ---------- statistics ----------

func computeStats(vals []float64) Stats {
	n := len(vals)
	if n == 0 {
		return Stats{}
	}
	sorted := make([]float64, n)
	copy(sorted, vals)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(n)

	variance := 0.0
	for _, v := range sorted {
		d := v - mean
		variance += d * d
	}
	variance /= float64(n)

	return Stats{
		Mean:   round2(mean),
		Stddev: round2(math.Sqrt(variance)),
		P50:    round2(percentile(sorted, 0.50)),
		P95:    round2(percentile(sorted, 0.95)),
		P99:    round2(percentile(sorted, 0.99)),
		Min:    round2(sorted[0]),
		Max:    round2(sorted[n-1]),
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// ---------- main ----------

func main() {
	modelRef := flag.String("model", "", "model reference, e.g. deepseek/deepseek-chat (default: first configured provider)")
	n := flag.Int("n", 100, "number of samples to collect")
	outPath := flag.String("out", "baseline.json", "output baseline JSON path")
	dryRun := flag.Bool("dry-run", false, "print config and exit without calling the API")
	verbose := flag.Bool("verbose", false, "include per-sample data in output")
	timeout := flag.Duration("timeout", 120*time.Second, "per-request timeout")
	flag.Parse()

	// 1. Load config (read-only, no disk mutation)
	cfg, err := config.LoadForRootReadOnly(".")
	if err != nil {
		fatal("load config: %v", err)
	}

	// 2. Resolve model
	var entry *config.ProviderEntry
	if *modelRef != "" {
		entry, err = resolveModel(cfg, *modelRef)
	} else {
		entry, err = firstProvider(cfg)
	}
	if err != nil {
		fatal("%v", err)
	}

	fmt.Fprintf(os.Stderr, "provider: %s  kind: %s  model: %s  base_url: %s\n",
		entry.Name, entry.Kind, entry.Model, entry.BaseURL)

	if *dryRun {
		fmt.Fprintln(os.Stderr, "[dry-run] config OK, exiting.")
		return
	}

	// 3. Build provider
	prov, err := boot.NewProvider(entry)
	if err != nil {
		fatal("build provider: %v", err)
	}

	// 4. Collect samples
	samples := make([]Sample, 0, *n)
	errCount := 0

	for i := 0; i < *n; i++ {
		prompt := prompts[i%len(prompts)]
		s := collectOne(prov, prompt, i, *timeout)
		if s.Error != "" {
			errCount++
			fmt.Fprintf(os.Stderr, "  [%3d/%d] ERROR: %s\n", i+1, *n, s.Error)
		} else {
			fmt.Fprintf(os.Stderr, "  [%3d/%d] latency=%6.0fms  ttft=%5.0fms  chars=%4d  tokens=%4d  finish=%s\n",
				i+1, *n, s.LatencyMs, s.TTFTMs, s.OutputChars, s.OutputTokens, s.FinishReason)
		}
		samples = append(samples, s)

		// small pause to avoid rate limits
		if i < *n-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// 5. Compute statistics (skip errored samples)
	var latencies, ttfts, chars, outToks, promptToks []float64
	for _, s := range samples {
		if s.Error != "" {
			continue
		}
		latencies = append(latencies, s.LatencyMs)
		ttfts = append(ttfts, s.TTFTMs)
		chars = append(chars, float64(s.OutputChars))
		outToks = append(outToks, float64(s.OutputTokens))
		promptToks = append(promptToks, float64(s.PromptTokens))
	}

	latStats := computeStats(latencies)
	ttftStats := computeStats(ttfts)

	baseline := Baseline{
		Model:       entry.Model,
		Provider:    entry.Name,
		N:           *n,
		Errors:      errCount,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		LatencyMs:   latStats,
		TTFTMs:      ttftStats,
		OutputChars: computeStats(chars),
		OutputTok:   computeStats(outToks),
		PromptTok:   computeStats(promptToks),
		Thresholds: map[string]float64{
			// 🔴 hard gates
			"latency_p99_red": round2(latStats.P99 * 5), // 5× P99 → red
			"ttft_p99_red":    round2(ttftStats.P99 * 5),
			// 🟡 soft gates
			"latency_mean_yellow": round2(latStats.Mean + 3*latStats.Stddev), // mean+3σ
			"ttft_mean_yellow":    round2(ttftStats.Mean + 3*ttftStats.Stddev),
			"output_chars_upper":  round2(computeStats(chars).Mean + 3*computeStats(chars).Stddev),
			"output_chars_lower":  round2(math.Max(0, computeStats(chars).Mean-3*computeStats(chars).Stddev)),
		},
	}
	if *verbose {
		baseline.Samples = samples
	}

	// 6. Write output
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		fatal("marshal: %v", err)
	}
	if err := os.WriteFile(*outPath, data, 0644); err != nil {
		fatal("write %s: %v", *outPath, err)
	}

	fmt.Fprintf(os.Stderr, "\n✅ baseline written to %s (%d samples, %d errors)\n", *outPath, *n, errCount)
	printSummary(baseline)
}

func collectOne(prov provider.Provider, prompt string, idx int, timeout time.Duration) Sample {
	s := Sample{Index: idx}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: prompt},
		},
		// No MaxTokens cap: let the model finish naturally so the baseline
		// measures real performance, not a truncation artifact. (DashScope
		// also returns all-zero usage on truncation, which would skew stats.)
	}

	start := time.Now()
	ch, err := prov.Stream(ctx, req)
	if err != nil {
		s.Error = fmt.Sprintf("stream start: %v", err)
		s.LatencyMs = float64(time.Since(start).Milliseconds())
		return s
	}

	var sb strings.Builder
	firstText := false

	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			if !firstText {
				s.TTFTMs = float64(time.Since(start).Milliseconds())
				firstText = true
			}
			sb.WriteString(chunk.Text)
		case provider.ChunkReasoning:
			// count reasoning tokens but don't include in output text
		case provider.ChunkUsage:
			if u := chunk.Usage; u != nil {
				// DashScope returns all-zero usage on a truncated response
				// (max_output_tokens hit): output_tokens/input_tokens/total_tokens
				// are all 0. Don't clobber real token counts with that; the
				// finish reason still tells us the stream ended.
				if u.TotalTokens == 0 && u.CompletionTokens == 0 && u.PromptTokens == 0 {
					if s.FinishReason == "" {
						s.FinishReason = u.FinishReason
					}
					break
				}
				s.OutputTokens = u.CompletionTokens
				s.PromptTokens = u.PromptTokens
				s.ReasoningTok = u.ReasoningTokens
				s.FinishReason = u.FinishReason
			}
		case provider.ChunkError:
			s.Error = fmt.Sprintf("stream error: %v", chunk.Err)
		}
	}

	s.LatencyMs = float64(time.Since(start).Milliseconds())
	s.OutputChars = sb.Len()

	if !firstText && s.Error == "" {
		s.Error = "no text output received"
	}
	if s.FinishReason == "" && s.Error == "" {
		s.FinishReason = "unknown"
	}
	return s
}

// ---------- helpers ----------

func resolveModel(cfg *config.Config, ref string) (*config.ProviderEntry, error) {
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, fmt.Errorf("model %q not found in config; available providers: %s",
			ref, providerNames(cfg))
	}
	return entry, nil
}

func firstProvider(cfg *config.Config) (*config.ProviderEntry, error) {
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("no providers configured; add one to reasonix.toml or pass -model")
	}
	e := cfg.Providers[0]
	e.Model = e.DefaultModel()
	return &e, nil
}

func providerNames(cfg *config.Config) string {
	names := make([]string, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

func printSummary(b Baseline) {
	fmt.Fprintf(os.Stderr, `
┌─────────────────────────────────────────────────┐
│  Baseline Summary: %s
├──────────────┬────────┬────────┬────────┬────────┤
│ Metric       │  Mean  │  P50   │  P95   │  P99   │
├──────────────┼────────┼────────┼────────┼────────┤
│ Latency (ms) │ %6.0f │ %6.0f │ %6.0f │ %6.0f │
│ TTFT    (ms) │ %6.0f │ %6.0f │ %6.0f │ %6.0f │
│ Out Chars    │ %6.0f │ %6.0f │ %6.0f │ %6.0f │
│ Out Tokens   │ %6.0f │ %6.0f │ %6.0f │ %6.0f │
└──────────────┴────────┴────────┴────────┴────────┘
`,
		b.Provider+"/"+b.Model,
		b.LatencyMs.Mean, b.LatencyMs.P50, b.LatencyMs.P95, b.LatencyMs.P99,
		b.TTFTMs.Mean, b.TTFTMs.P50, b.TTFTMs.P95, b.TTFTMs.P99,
		b.OutputChars.Mean, b.OutputChars.P50, b.OutputChars.P95, b.OutputChars.P99,
		b.OutputTok.Mean, b.OutputTok.P50, b.OutputTok.P95, b.OutputTok.P99,
	)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fatal: "+format+"\n", args...)
	os.Exit(1)
}
