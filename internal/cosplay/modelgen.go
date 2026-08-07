// Model-backed CoSPlay components: the offline TemplateGenerator is useful
// but cannot see the task's intent beyond the examples. ModelGenerator /
// ModelRepairer drive the same co-evolution engine with an LLM backend so
// discriminating tests and repairs are generated from the task description,
// matching the CoSPlay paper's "generate adversarial tests with the model"
// step. Both degrade gracefully to the offline templates when the model
// output cannot be parsed — the verification loop must never crash on
// model noise.
package cosplay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ModelBackend is the minimal LLM surface the model-backed components need:
// one non-streaming completion. The host adapts its provider to this
// interface (see ProviderBackend in internal/boot), keeping cosplay free of
// any provider dependency.
type ModelBackend interface {
	// Complete returns the model's full text for one request.
	Complete(ctx context.Context, system, user string, maxTokens int) (string, error)
}

// ModelGenerator is a TestGenerator that asks the backend to produce
// discriminating test cases. On any parse failure it falls back to
// TemplateGenerator so the verification loop always has tests.
type ModelGenerator struct {
	Backend ModelBackend
	// MaxTokens bounds the generation request (default 1200).
	MaxTokens int
}

// Generate implements TestGenerator.
func (g ModelGenerator) Generate(ctx context.Context, task Task, code string, n int) (tests []TestCase) {
	if n <= 0 {
		n = 4
	}
	backend := g.Backend
	if backend == nil || isNilBackend(backend) {
		return TemplateGenerator{}.Generate(ctx, task, code, n)
	}
	maxTokens := g.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1200
	}
	system := "You write discriminating test programs for code verification. " +
		"Return ONLY a JSON array of test cases. Each element is an object " +
		`{"input": <string representation of the input or "">, "expected": <string representation of the expected output>}. ` +
		"Tests must separate correct from incorrect implementations; prefer edge cases. No prose, no markdown fences."
	user := fmt.Sprintf("Language: %s\nTask: %s\n\nCode under test:\n```\n%s\n```\n\nGenerate %d test cases as JSON.", task.Language, task.Description, code, n)
	raw, err := backend.Complete(ctx, system, user, maxTokens)
	if err != nil {
		return TemplateGenerator{}.Generate(ctx, task, code, n)
	}
	parsed, err := parseTestCaseJSON(raw)
	if err != nil || len(parsed) == 0 {
		return TemplateGenerator{}.Generate(ctx, task, code, n)
	}
	for i, tc := range parsed {
		if i >= n {
			break
		}
		tc.ID = fmt.Sprintf("model-%d", i)
		tc.Language = task.Language
		tc.Source = "model"
		tests = append(tests, tc)
	}
	if len(tests) == 0 {
		return TemplateGenerator{}.Generate(ctx, task, code, n)
	}
	return tests
}

// ModelRepairer is a Repairer that asks the backend to fix a failing
// candidate against the observed failures. Returns "" on failure so the
// engine stops repairing.
type ModelRepairer struct {
	Backend ModelBackend
	// MaxTokens bounds the repair request (default 1500).
	MaxTokens int
}

// Repair implements Repairer.
func (r ModelRepairer) Repair(ctx context.Context, task Task, code string, fails []Failure, lang string) string {
	backend := r.Backend
	if backend == nil || isNilBackend(backend) {
		return ""
	}
	maxTokens := r.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1500
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Language: %s\nTask: %s\n\nThe following code fails the listed tests.\n", lang, task.Description)
	fmt.Fprintf(&b, "Code under test:\n```\n%s\n```\n\nFailures:\n", code)
	for _, f := range fails {
		detail := f.Detail
		if detail == "" {
			detail = fmt.Sprintf("input=%q expected=%q got=%q", f.Input, f.Expected, f.Got)
		}
		fmt.Fprintf(&b, "- %s: %s\n", f.TestID, detail)
	}
	b.WriteString("\nReturn the COMPLETE fixed code inside a single fenced code block (```lang ... ```). Nothing else.\n")
	system := "You fix code so it passes the failing tests. Return only the complete corrected source in one fenced code block."
	raw, err := backend.Complete(ctx, system, b.String(), maxTokens)
	if err != nil {
		return ""
	}
	fixed := extractFencedCode(raw)
	if fixed == "" || fixed == code {
		return ""
	}
	return fixed
}

// parseTestCaseJSON tolerates prose around the JSON array and a trailing
// comma (common model sloppiness).
func parseTestCaseJSON(raw string) ([]TestCase, error) {
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array in model output")
	}
	body := raw[start : end+1]
	// Strip a trailing comma before the closing bracket.
	if idx := strings.LastIndex(body[:len(body)-1], ","); idx >= 0 {
		tail := strings.TrimSpace(body[idx+1 : len(body)-1])
		if tail == "" {
			body = body[:idx] + body[len(body)-1:]
		}
	}
	var out []struct {
		Input    string `json:"input"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	res := make([]TestCase, 0, len(out))
	for _, e := range out {
		res = append(res, TestCase{Input: e.Input, Expected: e.Expected})
	}
	return res, nil
}

// extractFencedCode pulls the first fenced code block (```lang ... ```) out
// of a model response.
func extractFencedCode(raw string) string {
	lines := strings.Split(raw, "\n")
	var in bool
	var out []string
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			if in {
				in = false
				break
			}
			in = true
			continue
		}
		if in {
			out = append(out, ln)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func isNilBackend(b ModelBackend) bool {
	switch v := b.(type) {
	case nil:
		return true
	case interface{ IsNil() bool }:
		return v.IsNil()
	default:
		return false
	}
}
