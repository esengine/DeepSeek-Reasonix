package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

// AskTool obtains what only the user can supply: a decision that is theirs, or a
// value the request never carried. It is not a way to ask permission, not the
// plan card, and not a substitute for research the agent can do itself. It
// reaches the user through the Asker on the call context; a run with no asker
// leaves the decision unresolved rather than inventing an answer, because a run
// with nobody to ask is precisely a run that cannot settle this.
type AskTool struct{}

func NewAskTool() *AskTool { return &AskTool{} }

func (*AskTool) Name() string { return "ask" }

func (*AskTool) Description() string {
	return "Ask the user for something only they can supply: a decision that is theirs to make, or a value the request never gave you. Use it when two or more genuinely reasonable options would change the architecture, public API, data model, dependencies, UX, compatibility, scope, or anything irreversible — having a recommendation does not make such a choice yours to make, so put the recommended option first and still ask. Everything else is not a question for the user: whether you may touch a file is permission, whether a plan may run is the plan card, and where something lives or why a test failed is yours to go find out. Purely local, low-risk, reversible implementation details take the sensible default without asking. Calling this ends the round — anything you write alongside it will not run, because it would be reasoning from an answer you do not have yet. No tool-approval mode, YOLO included, answers for the user. Each question has a short `header` (a tab label), the `question` text, 2-4 `options` (each a `label` and optional `description`), and `multiSelect` when more than one may apply."
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
        "reason":{"type":"string","enum":["user_decision","missing_value"],"description":"Why only the user can answer: user_decision when they must pick between real alternatives, missing_value when the request never supplied something you need. Nothing else belongs here."},
        "multiSelect":{"type":"boolean","description":"Allow selecting more than one option."}
      },
      "required":["question","header","options"]
    }
  }
},
"required":["questions"]
}`)
}

// DecisionBarrier is true: the answer is the user's, and everything the model
// wrote alongside this call was written without it.
func (*AskTool) DecisionBarrier() bool { return true }

// ReadOnly is true: asking has no host side effects, so it never needs approval
// and stays available in plan mode (clarifying scope while planning is fine).
func (*AskTool) ReadOnly() bool { return true }

func (*AskTool) Reach() tool.Reach { return tool.ReachHostControl }

func (*AskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Questions []struct {
			Header      string `json:"header"`
			Question    string `json:"question"`
			Reason      string `json:"reason"`
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
			Reason:  askReason(q.Reason),
			Options: opts,
			Multi:   q.MultiSelect,
		})
	}

	_, _, asker, ok := CallContext(ctx)
	if !ok || asker == nil {
		// A run with nobody to ask cannot answer a question only the user may
		// answer. Telling the model to decide for itself would contradict the one
		// rule that puts a call here, so the decision stays unresolved.
		return "unresolved: this run has no interactive user, and the answer is the user's to give — " +
			"nothing here can supply it, including you. Do not choose on their behalf. Do whatever the task " +
			"allows without this decision, then call conclude_blocked naming the decision that is missing.", nil
	}

	answers, err := asker.Ask(ctx, qs)
	if err != nil {
		return "", fmt.Errorf("ask: %w", err)
	}
	return formatAnswers(qs, answers), nil
}

// formatAnswers renders the user's selections as a compact, model-facing summary,
// keyed by question header so the model can tell which answer is which. When the
// user picked nothing at all (the "just chat" / dismiss path), it returns an
// explicit stop signal instead of a per-question "(no answer)" — otherwise the
// model reads the empty result as license to proceed and acts unasked.
func formatAnswers(qs []event.AskQuestion, answers []event.AskAnswer) string {
	pick := make(map[string][]string, len(answers))
	for _, a := range answers {
		pick[a.QuestionID] = a.Selected
	}
	answered := 0
	for _, q := range qs {
		if len(pick[q.ID]) > 0 {
			answered++
		}
	}
	if answered == 0 {
		return "The user dismissed the question without choosing — read this as \"don't decide for me, let's just talk.\" Do not pick an option, run a tool, or take any further action toward this; stop and wait for the user's next message."
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
			fmt.Fprintf(&b, "- %s: (left unanswered — don't assume a choice)\n", label)
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, strings.Join(sel, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// askReason normalises the model's answer to the two the host recognises. An
// unstated or unknown reason reads as a decision, which is the reading that
// keeps a question in front of the user rather than quietly reclassifying it.
func askReason(raw string) string {
	if strings.TrimSpace(raw) == event.AskReasonMissingValue {
		return event.AskReasonMissingValue
	}
	return event.AskReasonUserDecision
}
