package cosplay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CodeVerifyTool is a reasonix tool that runs the CoSPlay co-evolution loop
// on a piece of code and returns the matrix/report as result text. It is the
// agent-facing entry point for inference-time code verification without
// ground-truth data: the model can call it after generating or editing code
// to get a discriminating test run plus the consensus best candidate.
type CodeVerifyTool struct {
	// Gen, Runner, Repair are the co-evolution components. When nil, defaults
	// are used: TemplateGenerator + ProcessRunner + no repairer (offline).
	Gen    TestGenerator
	Runner Runner
	Repair Repairer

	MaxRounds int
	NumTests  int
	Timeout   int // seconds; 0 = runner default
}

// verifyArgs is the tool's JSON parameter shape.
type verifyArgs struct {
	Language    string        `json:"language"`
	Code        string        `json:"code"`
	File        string        `json:"file"`
	Task        string        `json:"task"`
	Function    string        `json:"function"`
	Examples    []ExampleJSON `json:"examples"`
}

// ExampleJSON is the wire form of an input/output pair.
type ExampleJSON struct {
	Input    string `json:"input"`
	Expected string `json:"expected"`
}

// NewCodeVerifyTool builds the tool with defaults applied.
func NewCodeVerifyTool() *CodeVerifyTool {
	return &CodeVerifyTool{
		Gen:       TemplateGenerator{},
		Runner:    &ProcessRunner{},
		MaxRounds: 2,
		NumTests:  4,
	}
}

// Name implements tool.Tool.
func (t *CodeVerifyTool) Name() string { return "code_verify" }

// Description implements tool.Tool.
func (t *CodeVerifyTool) Description() string {
	return "Runs CoSPlay co-evolution verification on code without ground-truth data: generates discriminating tests, executes a code×test matrix over repair rounds, and returns the consensus best result. Pass language, code (or file), a task description, and optional input/expected examples."
}

// Schema implements tool.Tool.
func (t *CodeVerifyTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "language":{"type":"string","description":"Code language: go | python | javascript"},
  "code":{"type":"string","description":"The code to verify (function/type definitions; omit when file is set)."},
  "file":{"type":"string","description":"Path to a file containing the code (alternative to code)."},
  "task":{"type":"string","description":"What the code is supposed to do."},
  "function":{"type":"string","description":"Entry function name, if known."},
  "examples":{"type":"array","description":"Input/output pairs used to build discriminating tests.",
    "items":{"type":"object","properties":{
      "input":{"type":"string","description":"Invocation expression or argument, e.g. \"21\"."},
      "expected":{"type":"string","description":"Expected result, e.g. \"42\"."}}
    }}
},
"required":["language"]}`)
}

// ReadOnly implements tool.Tool — verification never mutates the host.
func (t *CodeVerifyTool) ReadOnly() bool { return true }

// Execute implements tool.Tool.
func (t *CodeVerifyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var a verifyArgs
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("code_verify: bad args: %v", err)
	}
	if strings.TrimSpace(a.Language) == "" {
		return "", fmt.Errorf("code_verify: language is required")
	}
	code := a.Code
	if strings.TrimSpace(code) == "" && strings.TrimSpace(a.File) != "" {
		data, err := os.ReadFile(a.File)
		if err != nil {
			return "", fmt.Errorf("code_verify: read %s: %v", a.File, err)
		}
		code = string(data)
	}
	if strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("code_verify: provide code or an existing file")
	}

	examples := make([]Example, 0, len(a.Examples))
	for _, e := range a.Examples {
		examples = append(examples, Example{Input: e.Input, Expected: e.Expected})
	}
	task := Task{
		Description: a.Task,
		Language:    a.Language,
		Function:    a.Function,
		Examples:    examples,
	}

	gen, runner := t.Gen, t.Runner
	if gen == nil {
		gen = TemplateGenerator{}
	}
	if runner == nil {
		runner = &ProcessRunner{}
	}
	v := NewVerifier(gen, runner, t.Repair)
	if t.MaxRounds > 0 {
		v.MaxRounds = t.MaxRounds
	}
	if t.NumTests > 0 {
		v.NumTests = t.NumTests
	}

	rep, err := v.Verify(ctx, task, Candidate{ID: "candidate", Code: code, Language: a.Language})
	if err != nil {
		return "", fmt.Errorf("code_verify: %v", err)
	}
	return formatReport(rep), nil
}

// formatReport renders a Verify report as concise result text for the model.
func formatReport(rep Report) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CoSPlay verification: %d/%d tests passed (%.0f%%), %d round(s)\n",
		rep.Passed, rep.Tests, rep.PassRate*100, rep.Rounds))
	if len(rep.Corrections) > 0 {
		b.WriteString("Repair rounds:\n")
		for _, c := range rep.Corrections {
			b.WriteString("  - " + c + "\n")
		}
	}
	if len(rep.Discarded) > 0 {
		b.WriteString(fmt.Sprintf("Pruned ineffective tests: %s\n", strings.Join(rep.Discarded, ", ")))
	}
	b.WriteString("Consensus best candidate:\n")
	b.WriteString(rep.Best.Code)
	if rep.PassRate < 1.0 {
		b.WriteString("\nNOTE: not all tests pass — refine the code or add more examples and re-run.\n")
	}
	return b.String()
}
