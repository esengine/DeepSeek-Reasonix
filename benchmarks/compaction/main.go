// CompactionBench measures what repeated compaction costs and what it loses.
// Both arms drive the real agent compaction path over a session that grows one
// generation at a time:
//
//	-mode=cost      offline: what each fold costs and whether any single
//	                summarizer call can still overflow the window
//	-mode=fidelity  real provider: which planted facts survive N folds,
//	                scored against a full-history control
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
	"unicode/utf8"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	_ "reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

const (
	realModel   = "deepseek-v4-flash"
	realBaseURL = "https://api.deepseek.com"
	// probeAnswerTokens must cover a thinking model's reasoning plus the short
	// answer; too small and every probe scores as lost.
	probeAnswerTokens = 2048
)

func main() {
	mode := flag.String("mode", "cost", "cost | fidelity")
	gens := flag.Int("gens", 8, "generations of work+compaction to run")
	report := flag.String("report", "1,2,4,8", "generations to report on")
	window := flag.Int("window", 128_000, "context window in tokens")
	control := flag.Bool("control", true, "fidelity: also score probes against full history")
	arm := flag.String("arm", "full", "full | incremental: re-derive each digest from canonical, or fold the previous projection")
	snip := flag.Bool("snip", false, "run the stale-tool-result snip pass before each fold, as the 0.6 threshold does in a real session")
	out := flag.String("out", "", "write the JSON report here")
	flag.Parse()

	var (
		res []genResult
		err error
	)
	a := arms{incremental: *arm == "incremental", snip: *snip}
	switch {
	case *arm != "full" && *arm != "incremental":
		err = fmt.Errorf("unknown arm %q", *arm)
	case *mode == "cost":
		res, err = runCost(*gens, *window, a)
	case *mode == "fidelity":
		res, err = runFidelity(*gens, *window, *control, a)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printReport(*mode+" / "+*arm, res, reportAt(*report))
	if *out != "" {
		b, _ := json.MarshalIndent(map[string]any{"mode": *mode, "arm": *arm, "window": *window, "generations": res}, "", "  ")
		if werr := os.WriteFile(*out, append(b, '\n'), 0o644); werr != nil {
			fmt.Fprintln(os.Stderr, werr)
			os.Exit(1)
		}
	}
}

// genResult is one generation: the fold that ran and what it cost or lost.
type genResult struct {
	Gen              int            `json:"gen"`
	CanonicalTokens  int            `json:"canonical_tokens"`
	ProjectionTokens int            `json:"projection_tokens"`
	SummarizerCalls  int            `json:"summarizer_calls"`
	SummarizerInput  int            `json:"summarizer_input_tokens"`
	LargestCall      int            `json:"largest_call_tokens"`
	Mode             string         `json:"mode,omitempty"`
	SnippedResults   int            `json:"snipped_results,omitempty"`
	SnippedChars     int            `json:"snipped_chars,omitempty"`
	Seconds          float64        `json:"seconds"`
	Error            string         `json:"error,omitempty"`
	Survived         map[string]int `json:"survived,omitempty"`   // probe class -> 1 kept, 0 lost
	ControlOK        map[string]int `json:"control_ok,omitempty"` // same probes against full history
	// What the model actually said, so a score can be audited rather than trusted.
	Answers        map[string]string `json:"answers,omitempty"`
	ControlAnswers map[string]string `json:"control_answers,omitempty"`
}

type harness struct {
	sess   *agent.Session
	agentA *agent.Agent
	path   string
	calls  *callRecorder
	snip   bool
}

func newHarness(t *testingDir, p provider.Provider, window int, rec *callRecorder, arm arms) *harness {
	sess := newSession()
	path := filepath.Join(t.dir, "session.jsonl")
	a := agent.New(p, tool.NewRegistry(), sess, agent.Options{
		ContextWindow: window,
		ArchiveDir:    filepath.Join(t.dir, "archive"),
		SessionPath:   path,
		RecentKeep:    4,
		// boot's default when cfg.Agent.Keep is unset; without it the bench
		// would measure a configuration no real session runs.
		KeepPolicy: agent.KeepErrors,
		Ablation:   foldArm(arm.incremental),
	}, rec.sink())
	return &harness{sess: sess, agentA: a, path: path, calls: rec, snip: arm.snip}
}

// foldArm switches full re-derivation off, which is what makes a fold read the
// previous projection instead of the canonical transcript.
// arms selects the maintenance behaviour under test. Snipping is off by default
// so a run stays comparable with baselines recorded before it existed.
type arms struct {
	incremental bool
	snip        bool
}

func foldArm(incremental bool) ablation.Set {
	if incremental {
		return ablation.New(ablation.FullFold)
	}
	return ablation.Set{}
}

// runGeneration grows the session and folds it, returning what that fold cost.
func (h *harness) runGeneration(ctx context.Context, gen int, probes []probe) genResult {
	growSession(h.sess, gen, probes)
	r := genResult{Gen: gen, CanonicalTokens: estimateTokens(renderAll(h.sess.Snapshot()))}

	h.calls.reset()
	start := time.Now()
	if h.snip {
		st, serr := h.agentA.SnipStaleToolResults()
		if serr != nil {
			r.Error = serr.Error()
		}
		r.SnippedResults, r.SnippedChars = st.Results, st.SavedChars
	}
	err := h.agentA.CompactNow(ctx, "")
	r.Seconds = time.Since(start).Seconds()
	if err != nil {
		r.Error = err.Error()
	}
	r.SummarizerCalls = len(h.calls.calls)
	for _, c := range h.calls.calls {
		r.SummarizerInput += c.tokens
		r.LargestCall = max(r.LargestCall, c.tokens)
	}
	if st, ok, sterr := agent.LoadCompactionState(h.path); sterr == nil && ok {
		r.ProjectionTokens = st.Projection.ProjectionTokens
		r.Mode = st.LastMode
	}
	return r
}

func runCost(gens, window int, a arms) ([]genResult, error) {
	dir, cleanup, err := tempDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	rec := &callRecorder{}
	p := &scriptedProvider{rec: rec, reply: syntheticDigest, window: window}
	h := newHarness(dir, p, window, rec, a)

	var out []genResult
	for gen := range gens {
		out = append(out, h.runGeneration(context.Background(), gen, probeSuite()))
	}
	return out, nil
}

func runFidelity(gens, window int, control bool, a arms) ([]genResult, error) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("fidelity mode needs DEEPSEEK_API_KEY")
	}
	p, err := provider.New("openai", provider.Config{Name: "compactionbench", BaseURL: realBaseURL, Model: realModel, APIKey: key})
	if err != nil {
		return nil, err
	}
	dir, cleanup, cerr := tempDir()
	if cerr != nil {
		return nil, cerr
	}
	defer cleanup()

	rec := &callRecorder{}
	h := newHarness(dir, &recordingProvider{inner: p, rec: rec}, window, rec, a)
	probes := probeSuite()

	ctx := context.Background()
	var out []genResult
	for gen := range gens {
		r := h.runGeneration(ctx, gen, probes)
		r.Survived, r.ControlOK = map[string]int{}, map[string]int{}
		r.Answers, r.ControlAnswers = map[string]string{}, map[string]string{}
		visible, verr := visibleContext(h.path, h.sess)
		if verr != nil {
			return nil, verr
		}
		for _, probe := range probes {
			if probe.settledAt() > gen {
				continue
			}
			answer, aerr := ask(ctx, p, visible, probe.question)
			if aerr != nil {
				return nil, fmt.Errorf("probe %s: %w", probe, aerr)
			}
			r.Survived[probe.class], r.Answers[probe.class] = boolToInt(probe.score(answer)), answer
			if control {
				full, ferr := ask(ctx, p, h.sess.Snapshot(), probe.question)
				if ferr != nil {
					return nil, fmt.Errorf("control %s: %w", probe, ferr)
				}
				r.ControlOK[probe.class], r.ControlAnswers[probe.class] = boolToInt(probe.score(full)), full
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// ask puts one probe question to the model on top of the given context. The
// budget has to clear the model's reasoning as well as its answer: a thinking
// model spends its first tokens reasoning, and a budget sized for the one-word
// answer alone comes back empty and scores as a fact compaction never lost.
func ask(ctx context.Context, p provider.Provider, msgs []provider.Message, question string) (string, error) {
	answer, reasoning, err := askOnce(ctx, p, msgs, question, probeAnswerTokens)
	if err != nil {
		return "", err
	}
	if answer == "" || strings.Contains(answer, toolCallMarker) {
		// One retry with room to think: a reply cut off mid-reasoning says
		// nothing about whether the fold kept the fact.
		answer, reasoning, err = askOnce(ctx, p, msgs, question, probeAnswerTokens*4)
		if err != nil {
			return "", err
		}
	}
	switch {
	case strings.Contains(answer, toolCallMarker):
		return toolCallInvalid, nil
	case answer == "":
		return fmt.Sprintf("%s: %d reasoning chars>", noAnswerMarker, reasoning), nil
	}
	return answer, nil
}

func askOnce(ctx context.Context, p provider.Provider, msgs []provider.Message, question string, budget int) (string, int, error) {
	req := provider.Request{
		Messages: append(append([]provider.Message(nil), provider.ModelMessages(msgs)...),
			provider.Message{Role: provider.RoleUser, Content: question + "\n\n" + probeAnswerContract}),
		MaxTokens: budget,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return "", 0, err
	}
	var answer, reasoning strings.Builder
	for c := range ch {
		switch c.Type {
		case provider.ChunkText:
			answer.WriteString(c.Text)
		case provider.ChunkReasoning:
			reasoning.WriteString(c.Text)
		case provider.ChunkError:
			return "", reasoning.Len(), c.Err
		}
	}
	return strings.TrimSpace(answer.String()), reasoning.Len(), nil
}

func printReport(mode string, res []genResult, at map[int]bool) {
	fmt.Printf("\n## CompactionBench (%s)\n\n", mode)
	fmt.Println("| gen | canonical tok | fold calls | fold input tok | largest call | projection tok | s | result |")
	fmt.Println("| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |")
	for _, r := range res {
		status := r.Mode
		if r.Error != "" {
			status = "ERROR: " + firstLine(r.Error)
		}
		fmt.Printf("| %d | %d | %d | %d | %d | %d | %.1f | %s |\n",
			r.Gen+1, r.CanonicalTokens, r.SummarizerCalls, r.SummarizerInput, r.LargestCall, r.ProjectionTokens, r.Seconds, status)
	}
	if !strings.HasPrefix(mode, "fidelity") {
		return
	}
	classes := probeSuite()
	fmt.Printf("\n### Probe survival (compacted / full-history control)\n\n| probe | %s |\n", joinGens(res, at))
	fmt.Printf("| --- | %s |\n", strings.Repeat(" ---: |", countGens(res, at)))
	for _, p := range classes {
		row := []string{}
		for _, r := range res {
			if !at[r.Gen+1] {
				continue
			}
			if _, asked := r.Survived[p.class]; !asked {
				row = append(row, "–")
				continue
			}
			row = append(row, fmt.Sprintf("%s/%s", mark(r.Survived[p.class], r.Answers[p.class]), mark(r.ControlOK[p.class], r.ControlAnswers[p.class])))
		}
		fmt.Printf("| %s | %s |\n", p.class, strings.Join(row, " | "))
	}
	printMeasurementQuality(res)
}

// printMeasurementQuality reports how many probes never got an answer at all.
// A survival rate quoted without it would read harness noise as fact loss.
func printMeasurementQuality(res []genResult) {
	asked, bad, badControl := 0, 0, 0
	for _, r := range res {
		for _, a := range r.Answers {
			asked++
			if invalidAnswer(a) {
				bad++
			}
		}
		for _, a := range r.ControlAnswers {
			if invalidAnswer(a) {
				badControl++
			}
		}
	}
	fmt.Printf("\nUnanswered probes (excluded from the rates above): %d of %d compacted, %d of %d control.\n", bad, asked, badControl, asked)
	if bad > 0 || badControl > 0 {
		fmt.Println("A run with unanswered probes measures the harness as much as the compactor; see answers in the JSON report.")
	}
}

func mark(v int, answer string) string {
	switch {
	case invalidAnswer(answer):
		return "n/a"
	case v == 1:
		return "ok"
	}
	return "LOST"
}

func joinGens(res []genResult, at map[int]bool) string {
	var s []string
	for _, r := range res {
		if at[r.Gen+1] {
			s = append(s, fmt.Sprintf("gen %d", r.Gen+1))
		}
	}
	return strings.Join(s, " | ")
}

func countGens(res []genResult, at map[int]bool) int {
	n := 0
	for _, r := range res {
		if at[r.Gen+1] {
			n++
		}
	}
	return n
}

func reportAt(spec string) map[int]bool {
	at := map[int]bool{}
	for part := range strings.SplitSeq(spec, ",") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &n); err == nil {
			at[n] = true
		}
	}
	return at
}

// estimateTokens mirrors the kernel's own estimator so bench numbers and
// compaction telemetry are read in the same unit.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	if runes := utf8.RuneCountInString(s); runes > (len(s)+3)/4 {
		return runes
	}
	return (len(s) + 3) / 4
}

func renderAll(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		for _, tc := range m.ToolCalls {
			b.WriteString(tc.Name)
			b.WriteString(tc.Arguments)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type testingDir struct{ dir string }

func tempDir() (*testingDir, func(), error) {
	dir, err := os.MkdirTemp("", "compactionbench-")
	if err != nil {
		return nil, nil, err
	}
	return &testingDir{dir: dir}, func() { _ = os.RemoveAll(dir) }, nil
}

// callRecorder captures every summarizer request the fold issued, which is the
// measurement the cost arm exists for: how many calls, and how large the
// largest one got.
type callRecorder struct{ calls []recordedCall }

type recordedCall struct {
	tokens int
	system string
}

func (r *callRecorder) reset() { r.calls = nil }

func (r *callRecorder) note(req provider.Request) {
	c := recordedCall{}
	for _, m := range req.Messages {
		c.tokens += estimateTokens(m.Content)
		if m.Role == provider.RoleSystem {
			c.system = m.Content
		}
	}
	r.calls = append(r.calls, c)
}

func (r *callRecorder) sink() event.Sink { return event.Discard }

const syntheticDigest = `## Standing facts & constraints
- never modify config/schema.sql
## Goal
Fix the config round-trip formatting bug.
## Pending & next step
Re-run TestRoundTrip after the latest edit.`

// scriptedProvider answers every summarizer call with a fixed digest so the
// cost arm is deterministic and needs no API key. It refuses an input larger
// than the window the way a real provider does, so the bench observes the
// wedge — a fold that can no longer be summarized at all — instead of
// inferring it from the input size.
type scriptedProvider struct {
	rec    *callRecorder
	reply  string
	window int
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.rec.note(req)
	ch := make(chan provider.Chunk, 2)
	if in := p.rec.calls[len(p.rec.calls)-1].tokens; p.window > 0 && in > p.window {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("this model's maximum context length is %d tokens, however you requested %d tokens", p.window, in)}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.reply}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// recordingProvider measures the same thing against a real provider.
type recordingProvider struct {
	inner provider.Provider
	rec   *callRecorder
}

func (p *recordingProvider) Name() string { return p.inner.Name() }

func (p *recordingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.rec.note(req)
	return p.inner.Stream(ctx, req)
}
