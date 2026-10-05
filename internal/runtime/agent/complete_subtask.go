package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// CompleteSubtaskTool is visible only inside sub-agent registries. It ends a
// delegated run with a structured, host-checkable claim instead of prose. It is
// never registered on the parent agent's tool surface.
type CompleteSubtaskTool struct{}

func NewCompleteSubtaskTool() *CompleteSubtaskTool { return &CompleteSubtaskTool{} }

// completeSubtaskToolName is the one spelling the contract, the registry and
// the telemetry all mean.
const completeSubtaskToolName = "complete_subtask"

func (*CompleteSubtaskTool) Name() string { return completeSubtaskToolName }

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
	// The submission always succeeds; what varies is the verdict. A rejection
	// the parent never learns about serves it worse than a lowered claim it can
	// see, so the call stands and the closure decision is said out loud.
	adjudicated, reasons := led.AdjudicateCompletion(report)
	closed := len(reasons) == 0
	led.NoteClosureVerdict(closed)
	if closed {
		return fmt.Sprintf("complete_subtask closed: status=%s criteria=%d unresolved=%d — the sub-task is done and nothing further is asked of you",
			adjudicated.Status, len(adjudicated.Criteria), len(adjudicated.Unresolved)), nil
	}
	// The host knows which claims it could not back and why; reporting only the
	// count leaves the sub-agent guessing which criterion and what would fix it.
	return fmt.Sprintf("complete_subtask needs_work: status=%s criteria=%d unresolved=%d — these claims are not backed by anything the host observed:\n- %s\nDo that work, then submit again.",
		adjudicated.Status, len(adjudicated.Criteria), len(adjudicated.Unresolved), strings.Join(reasons, "\n- ")), nil
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

// CompleteSubtaskContract is appended to a sub-agent's task prompt when the
// host expects a typed completion claim. The profile body says how to work;
// this states the non-negotiable closing protocol.
const CompleteSubtaskContract = `<completion-contract>
Call complete_subtask when you believe this sub-task is done. State the acceptance
criteria you were held to and attach, for each, the command you ran or the paths you
changed or inspected. The host checks every citation against what it observed you
actually do, and answers with a verdict: needs_work means a claim is not backed by
anything it saw — do that work and submit again; closed means the sub-task is done and
nothing further is asked of you. Cite real work only and put anything you assumed
rather than verified in unresolved.
</completion-contract>`
