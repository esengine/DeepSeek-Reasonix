//go:build live

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

const liveArgumentLabel = "中文🙂\nsecond line"

type liveArgumentTool struct {
	mu      sync.Mutex
	calls   []string
	invalid bool
}

func (*liveArgumentTool) Name() string { return "inspect_marker" }
func (*liveArgumentTool) Description() string {
	return "Inspect the requested marker. The unavailable marker returns an ordinary read error; alpha and beta return their marker."
}
func (*liveArgumentTool) ReadOnly() bool { return true }
func (*liveArgumentTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"marker":{"type":"string","enum":["alpha","beta","unavailable"]},"label":{"type":"string"}},"required":["marker","label"],"additionalProperties":false}`)
}
func (p *liveArgumentTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var v struct {
		Marker string `json:"marker"`
		Label  string `json:"label"`
	}
	if err := json.Unmarshal(args, &v); err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, v.Marker)
	if v.Label != liveArgumentLabel {
		p.invalid = true
		return "", fmt.Errorf("label did not preserve requested Unicode/newline")
	}
	if v.Marker == "unavailable" {
		return "", deterministicApplicationError("synthetic read failure: marker unavailable")
	}
	if v.Marker != "alpha" && v.Marker != "beta" {
		p.invalid = true
		return "", fmt.Errorf("unexpected marker")
	}
	return "marker-" + v.Marker, nil
}

// Parameters contain Unicode/escapes and the model must continue after an
// ordinary read error. No shell, credentials reader or external side effect.
func TestLiveMultiProviderArgumentsAndToolError(t *testing.T) {
	for _, tc := range multiProviderCases() {
		if os.Getenv(tc.keyEnv) == "" {
			continue
		}
		t.Run(tc.vendor+"/"+tc.model+"/"+tc.protocol, func(t *testing.T) {
			p := tc.new(t, "", "baseline")
			reg := tool.NewRegistry()
			probe := &liveArgumentTool{}
			reg.Add(probe)
			sess := NewSession("Use inspect_marker for each requested marker exactly once. An unavailable marker is an ordinary read error; continue the remaining reads without repeating it. Preserve literal argument strings exactly.")
			sink := &recordSink{}
			a := New(p, reg, sess, Options{MaxSteps: 6, MaxOutputTokens: 4096, MissingReasoningWarnStateDir: t.TempDir()}, sink)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			labelJSON, _ := json.Marshal(liveArgumentLabel)
			err := a.Run(ctx, fmt.Sprintf("Call inspect_marker for unavailable, alpha, and beta, exactly once each. Decode the JSON string %s and use its value as label in every call. Then summarize the two available markers and the unavailable result.", labelJSON))
			probe.mu.Lock()
			defer probe.mu.Unlock()
			counts := map[string]int{}
			for _, c := range probe.calls {
				counts[c]++
			}
			requests, prompt, completion := 0, 0, 0
			unknown := false
			for _, e := range sink.kinds(event.Usage) {
				if u := e.Usage; u != nil {
					requests += u.RequestCount
					prompt += u.PromptTokens
					completion += u.CompletionTokens
					unknown = unknown || u.Unknown
				}
			}
			t.Logf("provider=%s model=%s protocol=%s requests=%d prompt=%d completion=%d usage_unknown=%t calls=%v retries=%d", tc.vendor, tc.model, tc.protocol, requests, prompt, completion, unknown, counts, len(sink.kinds(event.Retrying)))
			if err != nil {
				t.Fatal(err)
			}
			if probe.invalid || counts["unavailable"] != 1 || counts["alpha"] != 1 || counts["beta"] != 1 {
				t.Fatalf("argument/read-error continuation mismatch: invalid=%t calls=%v", probe.invalid, counts)
			}
			m := sess.Snapshot()
			if len(m) == 0 || !strings.Contains(m[len(m)-1].Content, "alpha") || !strings.Contains(m[len(m)-1].Content, "beta") {
				t.Fatal("final answer lost successful read results")
			}
		})
	}
}
