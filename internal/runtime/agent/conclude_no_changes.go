package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

// ConcludeNoChangesTool is the planner's other structured exit. A turn that
// needs no work is the absence of a plan, not a plan with a flag, so it gets
// its own call — otherwise the host would be back to reading the conclusion out
// of the planner's prose.
type ConcludeNoChangesTool struct{}

func NewConcludeNoChangesTool() *ConcludeNoChangesTool { return &ConcludeNoChangesTool{} }

func (*ConcludeNoChangesTool) Name() string { return "conclude_no_changes" }

func (*ConcludeNoChangesTool) Description() string {
	return "End the planning turn with the conclusion that nothing needs to change, instead of submitting a plan. Call this when your research shows the request is already satisfied, or when the task turns out to need an answer rather than an edit. Give the `reason` the user should see — it becomes the reply. Do NOT call this and submit_plan in the same turn; the last call wins."
}

func (*ConcludeNoChangesTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "reason":{"type":"string","description":"What you found, and why no change is needed. This is shown to the user as the answer."}
},
"required":["reason"]
}`)
}

func (*ConcludeNoChangesTool) ReadOnly() bool { return true }

// ProviderVisible mirrors submit_plan: the schema stays constant for cache
// stability and availability is decided when the call runs.
func (*ConcludeNoChangesTool) ProviderVisible(ctx context.Context) bool {
	_, ok := planSubmissionFromContext(ctx)
	return ok
}

func (*ConcludeNoChangesTool) Unavailable(context.Context) tool.Refusal {
	return tool.Refusal{
		Code:    codeNoPlanningTurn,
		Message: "conclude_no_changes is only available while a planning turn is running — it is how a plan ends when nothing needs changing, not a way to end an execution turn",
	}
}

func (*ConcludeNoChangesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	submission, ok := planSubmissionFromContext(ctx)
	if !ok {
		return "", tool.Refusal{
			Code:    codeNoPlanningTurn,
			Message: "conclude_no_changes is only available while planning; there is no planning turn to conclude in this phase",
		}
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		return "", fmt.Errorf("conclude_no_changes needs a reason: state what you found and why no change is needed")
	}
	submission.recordNoChanges(reason)
	return "Recorded: no changes needed. The turn ends here; do not also submit a plan.", nil
}
