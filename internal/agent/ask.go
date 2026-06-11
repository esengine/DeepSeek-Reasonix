package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/event"
)

// AskTool lets the model put a structured multiple-choice question (or a few) to
// the user mid-task and get the answer back — for genuine forks the model can't
// resolve from the request or the code (which library, which approach, …) rather
// than guessing or asking in prose. The frontend renders selectable options, the
// user picks, and the choices come back as the tool result. It reaches the user
// through the Asker carried on the call
// context (CallContext); with no asker (headless runs) it returns an explicit
// model-assumption fallback so an autonomous run never blocks or pretends a user
// answered.
type AskTool struct{}

func NewAskTool() *AskTool { return &AskTool{} }

func (*AskTool) Name() string { return "ask" }

func (*AskTool) Description() string {
	return "Ask the user one or more multiple-choice questions when you hit a decision that is genuinely theirs to make — one you can't resolve from the request, the code, or sensible defaults. The frontend shows the options for the user to pick; their choices are returned to you. Prefer this over asking in prose for any real fork (which approach, which library, scope). Don't use it for decisions with an obvious default — pick the sensible option and proceed. Tool-approval modes such as YOLO do not answer these questions for the user. Each question has a short `header` (a tab label), the `question` text, 2-4 `options` (each a `label` and optional `description`; put any recommended option first), and `multiSelect` when more than one may apply."
}

func (*AskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "questions":{
    "type":"array",
    "minItems":1,
    "maxItems":4,
    "description":"1-4 questions to ask together.",
    "items":{
      "type":"object",
      "properties":{
        "header":{"type":"string","description":"Very short label for the question (a tab title), e.g. \"Library\"."},
        "question":{"type":"string","description":"The full question to ask."},
        "options":{
          "type":"array","minItems":2,"maxItems":4,
          "description":"The choices. Put any recommended option first.",
          "items":{
            "type":"object",
            "properties":{
              "label":{"type":"string","description":"The choice text (concise)."},
              "description":{"type":"string","description":"Optional one-line explanation of the choice."}
            },
            "required":["label"]
          }
        },
        "multiSelect":{"type":"boolean","description":"Allow selecting more than one option."}
      },
      "required":["question","header","options"]
    }
  }
},
"required":["questions"]
}`)
}

// ReadOnly is true: asking has no host side effects, so it never needs approval
// and stays available in plan mode (clarifying scope while planning is fine).
func (*AskTool) ReadOnly() bool { return true }

func (*AskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Questions []struct {
			Header      string `json:"header"`
			Question    string `json:"question"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if len(p.Questions) == 0 {
		return "", fmt.Errorf("at least one question is required")
	}

	qs := make([]event.AskQuestion, 0, len(p.Questions))
	for i, q := range p.Questions {
		question := strings.TrimSpace(q.Question)
		if question == "" || len(q.Options) < 2 {
			return "", fmt.Errorf("question %d: a question and at least two options are required", i+1)
		}
		opts := make([]event.AskOption, len(q.Options))
		seenLabels := make(map[string]int, len(q.Options))
		for j, o := range q.Options {
			label := strings.TrimSpace(o.Label)
			if label == "" {
				return "", fmt.Errorf("question %d option %d: label is required", i+1, j+1)
			}
			if prev, ok := seenLabels[label]; ok {
				return "", fmt.Errorf("question %d option %d: duplicate label %q also used by option %d", i+1, j+1, label, prev+1)
			}
			seenLabels[label] = j
			opts[j] = event.AskOption{Label: label, Description: strings.TrimSpace(o.Description)}
		}
		qs = append(qs, event.AskQuestion{
			ID:      fmt.Sprintf("q%d", i+1),
			Header:  strings.TrimSpace(q.Header),
			Prompt:  question,
			Options: opts,
			Multi:   q.MultiSelect,
		})
	}

	_, _, asker, ok := CallContext(ctx)
	if !ok || asker == nil {
		// Headless / no interactive user: don't block an autonomous run, but make
		// the provenance explicit so the model doesn't treat this as a user choice.
		return "No interactive user answered. This is a model-assumption fallback, not a user answer. Proceed with your best judgment, state the assumption you made, and prefer the safest reversible option when choices differ in risk.", nil
	}

	answers, err := asker.Ask(ctx, qs)
	if err != nil {
		return "", fmt.Errorf("ask: %w", err)
	}
	return formatAnswers(qs, answers), nil
}

// formatAnswers renders the user's selections as a compact, model-facing summary,
// keyed by question header so the model can tell which answer is which.
func formatAnswers(qs []event.AskQuestion, answers []event.AskAnswer) string {
	pick := make(map[string][]string, len(answers))
	for _, a := range answers {
		pick[a.QuestionID] = a.Selected
	}
	var b strings.Builder
	b.WriteString("The user answered:\n")
	for _, q := range qs {
		sel := pick[q.ID]
		label := q.Header
		if label == "" {
			label = q.Prompt
		}
		if len(sel) == 0 {
			fmt.Fprintf(&b, "- %s: (no answer)\n", label)
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, strings.Join(sel, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
