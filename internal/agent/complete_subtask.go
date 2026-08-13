package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// CompleteSubtaskTool is visible only inside sub-agent registries. It ends a
// delegated run with a structured, host-checkable claim instead of prose. It is
// never registered on the parent agent's tool surface.
type CompleteSubtaskTool struct{}

func NewCompleteSubtaskTool() *CompleteSubtaskTool { return &CompleteSubtaskTool{} }

func (*CompleteSubtaskTool) Name() string { return "complete_subtask" }

func (*CompleteSubtaskTool) Description() string {
	return "Close out this delegated sub-task with a structured result the parent can verify. Call once, last. status is complete, partial, blocked, or failed; summary states what is now true; acceptance_criteria lists each condition with the evidence for it; unresolved lists what you did not finish. The host checks every cited command and path against what it actually observed you do, and lowers any claim it cannot back."
}

// ReadOnly is true: submitting a report changes no workspace state. It is the
// claim itself, not a mutation.
func (*CompleteSubtaskTool) ReadOnly() bool { return true }

func (*CompleteSubtaskTool) Schema() json.RawMessage {
	// Fixed schema — sub-agent registries only, so it never enters the parent
	// prefix.
	return json.RawMessage(`{
"type":"object",
"properties":{
  "status":{"type":"string","description":"complete | partial | blocked | failed. Claim the truth: the host lowers a status its receipts cannot back."},
  "summary":{"type":"string","description":"What is now true as a result of this sub-task."},
  "acceptance_criteria":{
    "type":"array",
    "description":"Each condition this sub-task had to meet, with proof.",
    "items":{
      "type":"object",
      "properties":{
        "id":{"type":"string","description":"Short stable id, e.g. AC1."},
        "status":{"type":"string","description":"satisfied | unsatisfied"},
        "evidence":{
          "type":"array",
          "items":{
            "type":"object",
            "properties":{
              "kind":{"type":"string","enum":["verification","review","diff","files","manual"],"description":"verification = a command was run (command REQUIRED); review = a review completed; diff = a code change (paths REQUIRED); files = files created/edited/inspected (paths REQUIRED); manual = a manual check, which the host cannot back on its own."},
              "summary":{"type":"string","description":"The evidence itself."},
              "command":{"type":"string","description":"REQUIRED for verification: the command as it actually ran."},
              "paths":{"type":"array","items":{"type":"string"},"description":"REQUIRED for diff/files: the files this evidence refers to."}
            },
            "required":["kind","summary"]
          }
        }
      },
      "required":["id","status"]
    }
  },
  "unresolved":{"type":"array","items":{"type":"string"},"description":"What remains undone, unverified, or blocked. Put anything you assumed rather than checked here."}
},
"required":["status","summary"]
}`)
}

func (*CompleteSubtaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	report, err := evidence.ParseCompletionReport(args)
	if err != nil {
		return "", err
	}
	led, ok := evidence.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("complete_subtask requires the host evidence ledger; submit it from inside a sub-agent run")
	}
	// The submission always succeeds: the parent is better served by a claim it
	// can see the host lower than by a rejection the parent never learns about.
	adjudicated, reasons := led.AdjudicateCompletion(report)
	msg := fmt.Sprintf("complete_subtask accepted: status=%s criteria=%d unresolved=%d",
		adjudicated.Status, len(adjudicated.Criteria), len(adjudicated.Unresolved))
	if len(reasons) > 0 {
		msg += fmt.Sprintf(" — the host lowered %d unbacked criterion claim(s); run the check or cite what you really did", len(reasons))
	}
	return msg, nil
}

// AttachCompleteSubtaskTool adds complete_subtask to a sub-agent registry.
// Parent registries never receive it.
func AttachCompleteSubtaskTool(reg *tool.Registry) {
	if reg == nil {
		return
	}
	reg.Add(NewCompleteSubtaskTool())
}

// CompletionReport returns this agent's adjudicated completion claim, if it
// submitted one. Adjudication re-runs here so the returned status reflects the
// full run, including receipts recorded after the tool call itself.
func (a *Agent) CompletionReport() (evidence.CompletionReport, []string, bool) {
	if a == nil || a.task.ledger == nil {
		return evidence.CompletionReport{}, nil, false
	}
	report, ok := a.task.ledger.LatestCompletionReport()
	if !ok {
		return evidence.CompletionReport{}, nil, false
	}
	adjudicated, reasons := a.task.ledger.AdjudicateCompletion(report)
	return adjudicated, reasons, true
}

// completeSubtaskContract is appended to a sub-agent's task prompt when the
// host expects a typed completion claim. The profile body says how to work;
// this states the non-negotiable closing protocol.
const completeSubtaskContract = `<completion-contract>
End this sub-task by calling complete_subtask exactly once, as your final tool call.
State the acceptance criteria you were held to and attach, for each, the command you
ran or the paths you changed. The host checks every citation against what it observed
you actually do and lowers any claim it cannot back, so cite real work only and put
anything you assumed rather than verified in unresolved.
</completion-contract>`
