// Drives the context-maintenance E2E scenarios against the real DeepSeek API:
// seed → (idle past cache TTL) → resume A/B-compares cold-restart miss tokens with and without pruning.
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
	"reasonix/internal/tool/builtin"
)

const (
	model      = "deepseek-v4-flash"
	baseURL    = "https://api.deepseek.com"
	fatResults = 20
	fatBytes   = 12_000
)

func prov() provider.Provider {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "DEEPSEEK_API_KEY not set")
		os.Exit(1)
	}
	p, err := provider.New("openai", provider.Config{Name: "e2e", BaseURL: baseURL, Model: model, APIKey: key})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return p
}

func fakeGoFile(nonce string, i, size int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// module %s file%02d\npackage stress\n\n", nonce, i)
	line := 0
	for b.Len() < size {
		fmt.Fprintf(&b, "func helper_%s_%02d_%04d(x int) int { return x*%d + %d }\n", nonce, i, line, line+3, line*7)
		line++
	}
	return b.String()
}

func seedSession(nonce string) *agent.Session {
	s := agent.NewSession("You are a terse coding agent reviewing a Go codebase.")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "Review every file in module " + nonce + " one by one. Keep notes short."})
	for i := 0; i < fatResults; i++ {
		id := fmt.Sprintf("c%02d", i)
		name := fmt.Sprintf("src/file%02d.go", i)
		s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":%q}`, name)}}})
		s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: fakeGoFile(nonce, i, fatBytes)})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("Reviewed %s.", name)})
	}
	return s
}

// oneShot appends a user message and runs a single completion, returning usage.
func oneShot(p provider.Provider, msgs []provider.Message, question string) (provider.Usage, error) {
	req := provider.Request{
		Messages:  append(append([]provider.Message(nil), msgs...), provider.Message{Role: provider.RoleUser, Content: question}),
		MaxTokens: 32,
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		return provider.Usage{}, err
	}
	var u provider.Usage
	for c := range ch {
		switch c.Type {
		case provider.ChunkUsage:
			u = *c.Usage
		case provider.ChunkError:
			return u, c.Err
		}
	}
	return u, nil
}

type meta struct {
	SeededAt time.Time                 `json:"seeded_at"`
	Nonces   map[string]string         `json:"nonces"`
	SeedUse  map[string]provider.Usage `json:"seed_usage"`
}

func seed(dir string) {
	p := prov()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m := meta{SeededAt: time.Now(), Nonces: map[string]string{}, SeedUse: map[string]provider.Usage{}}
	for _, arm := range []string{"pruned", "control"} {
		nonce := fmt.Sprintf("%s%d", arm, time.Now().UnixNano()%1_000_000)
		s := seedSession(nonce)
		u, err := oneShot(p, s.Snapshot(), "How many files have you reviewed so far? Reply with just the number.")
		if err != nil {
			fmt.Fprintf(os.Stderr, "seed %s: %v\n", arm, err)
			os.Exit(1)
		}
		if err := s.Save(filepath.Join(dir, arm+".jsonl")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		m.Nonces[arm] = nonce
		m.SeedUse[arm] = u
		fmt.Printf("seeded %-7s prompt=%d hit=%d miss=%d\n", arm, u.PromptTokens, u.CacheHitTokens, u.CacheMissTokens)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resume(dir string) {
	p := prov()
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
	idle := time.Since(m.SeededAt).Round(time.Minute)

	out := map[string]provider.Usage{}
	for _, arm := range []string{"pruned", "control"} {
		s, err := agent.LoadSession(filepath.Join(dir, arm+".jsonl"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		prunes := 0
		if arm == "pruned" {
			a := agent.New(nil, tool.NewRegistry(), s, agent.Options{ContextWindow: 1_000_000, ArchiveDir: filepath.Join(dir, "archive")}, event.Discard)
			st, err := a.PruneStaleToolResults()
			if err != nil {
				fmt.Fprintln(os.Stderr, "prune:", err)
				os.Exit(1)
			}
			prunes = st.Results
		}
		u, err := oneShot(p, s.Snapshot(), "Which file did you review first? Reply with just the path.")
		if err != nil {
			fmt.Fprintf(os.Stderr, "resume %s: %v\n", arm, err)
			os.Exit(1)
		}
		out[arm] = u
		fmt.Printf("resume %-7s idle=%s pruned=%d prompt=%d hit=%d miss=%d\n", arm, idle, prunes, u.PromptTokens, u.CacheHitTokens, u.CacheMissTokens)
	}
	b, _ := json.MarshalIndent(map[string]any{"idle": idle.String(), "resume_usage": out}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("resume-%d.json", time.Now().Unix())), b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	c, pr := out["control"], out["pruned"]
	if c.CacheMissTokens > 0 {
		fmt.Printf("\ncold-restart miss tokens: control=%d pruned=%d (%.0f%% reduction)\n",
			c.CacheMissTokens, pr.CacheMissTokens, 100*(1-float64(pr.CacheMissTokens)/float64(c.CacheMissTokens)))
	}
}

// comprehension checks that the agent re-reads a file behind a prune placeholder
// instead of hallucinating: the answer is a number that exists only in the file.
func comprehension(trials int) {
	p := prov()
	pass := 0
	for t := 0; t < trials; t++ {
		dir, err := os.MkdirTemp("", "cm-e2e-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		secret := fmt.Sprintf("%d", 1000+time.Now().UnixNano()%9000)
		content := "package cfg\n\n// retention floor, milliseconds\nconst cacheRetentionFloor = " + secret + "\n" + strings.Repeat("// padding line filler for prune eligibility\n", 400)
		if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte(content), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		s := agent.NewSession("You are a terse coding agent. Use tools when you need file contents.")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "Read config.go and note its constants."})
		s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"config.go"}`}}})
		s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "r1", Name: "read_file", Content: content})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "Noted the constants in config.go."})
		for i := 0; i < 4; i++ {
			s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("ack %d", i)})
			s.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
		}

		reg := tool.NewRegistry()
		for _, tl := range (builtin.Workspace{Dir: dir}).Tools("read_file") {
			reg.Add(tl)
		}
		a := agent.New(p, reg, s, agent.Options{ContextWindow: 2000, RecentKeep: 2, MaxSteps: 5}, event.Discard)
		st, err := a.PruneStaleToolResults()
		if err != nil || st.Results == 0 {
			fmt.Fprintf(os.Stderr, "trial %d: prune did not fire (st=%+v err=%v)\n", t, st, err)
			os.Exit(1)
		}

		err = a.Run(context.Background(), "What is the exact numeric value of cacheRetentionFloor in config.go? Reply with just the number.")
		reRead, answered := false, false
		for _, msg := range s.Snapshot() {
			if msg.Role == provider.RoleTool && strings.Contains(msg.Content, "cacheRetentionFloor = "+secret) {
				reRead = true
			}
			if msg.Role == provider.RoleAssistant && strings.Contains(msg.Content, secret) {
				answered = true
			}
		}
		ok := err == nil && reRead && answered
		if ok {
			pass++
		}
		fmt.Printf("trial %d: re-read=%v answered=%v err=%v\n", t, reRead, answered, err)
		os.RemoveAll(dir)
	}
	fmt.Printf("\ncomprehension: %d/%d passed\n", pass, trials)
	if pass < trials {
		os.Exit(1)
	}
}

func main() {
	dir := flag.String("dir", "benchmarks/context-maintenance-e2e/run", "state directory for seed/resume")
	trials := flag.Int("trials", 5, "comprehension trials")
	flag.Parse()
	switch flag.Arg(0) {
	case "seed":
		seed(*dir)
	case "resume":
		resume(*dir)
	case "comprehension":
		comprehension(*trials)
	default:
		fmt.Fprintln(os.Stderr, "usage: context-maintenance-e2e [seed|resume|comprehension]")
		os.Exit(1)
	}
}
