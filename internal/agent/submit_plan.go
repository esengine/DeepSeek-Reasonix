package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/plancontract"
)

// SubmitPlanTool is the planner's structured exit: it hands the host a plan as
// data instead of prose the host would have to parse back. Validation runs here,
// so a malformed plan returns an actionable tool error the planner can fix in
// the next round rather than a silent misparse downstream.
type SubmitPlanTool struct{}

func NewSubmitPlanTool() *SubmitPlanTool { return &SubmitPlanTool{} }

func (*SubmitPlanTool) Name() string { return "submit_plan" }

func (*SubmitPlanTool) Description() string {
	return "Submit your finished plan as structured data. This is how a plan reaches the host — the host renders it for the user and hands it to the executor, so do NOT also restate the plan in prose. Every step needs a `title`; a step with a `parent_id` is a sub-step of that phase (two levels, keep phases few). Record what you actually READ as `verified_files` and what you only INFERRED as `candidate_files` — never present a guess as a verified path. Attach `acceptance` criteria and command-level `verification` to the steps they belong to, mark must-keep-passing behavior with `regression`, and label anything unproven in `assumptions`. Set `requires_approval` when execution should stop for the user first; the host decides whether it actually gates."
}

func (*SubmitPlanTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "objective":{"type":"string","description":"What this plan achieves, in one sentence."},
  "assumptions":{
    "type":"array",
    "description":"Premises the plan rests on that you did NOT verify. Label them here instead of stating them as facts.",
    "items":{
      "type":"object",
      "properties":{
        "text":{"type":"string","description":"The unverified premise."},
        "confirm":{"type":"string","description":"The cheapest check that would settle it."}
      },
      "required":["text"]
    }
  },
  "non_goals":{"type":"array","items":{"type":"string"},"description":"Explicitly out of scope."},
  "steps":{
    "type":"array",
    "minItems":1,
    "description":"Ordered steps. Top-level steps are phases; give a step a parent_id to make it a sub-step of that phase.",
    "items":{
      "type":"object",
      "properties":{
        "id":{"type":"string","description":"Short id for this step, e.g. \"s1\". Only needed when another step references it."},
        "parent_id":{"type":"string","description":"The id of the phase this step belongs to. Omit for a phase."},
        "title":{"type":"string","description":"Imperative description of the step."},
        "depends_on":{"type":"array","items":{"type":"string"},"description":"Ids of sibling steps that must happen first."},
        "verified_files":{"type":"array","items":{"type":"string"},"description":"Paths you actually opened and read."},
        "candidate_files":{"type":"array","items":{"type":"string"},"description":"Paths you inferred but did NOT read. Never list an unread path as verified."},
        "acceptance":{
          "type":"array",
          "description":"Checkable conditions this step must meet.",
          "items":{
            "type":"object",
            "properties":{
              "text":{"type":"string","description":"The condition, stated so it can be checked."},
              "regression":{"type":"boolean","description":"True when this is existing behavior that must keep working."},
              "optional":{"type":"boolean","description":"True for a nice-to-have that must never block completion."}
            },
            "required":["text"]
          }
        },
        "verification":{
          "type":"array",
          "description":"Commands that prove the step.",
          "items":{
            "type":"object",
            "properties":{
              "command":{"type":"string","description":"The command as the executor should run it."},
              "expect":{"type":"string","description":"What a pass looks like."}
            }
          }
        },
        "risks":{"type":"array","items":{"type":"string"},"description":"What could go wrong in this step."}
      },
      "required":["title"]
    }
  },
  "requires_approval":{"type":"boolean","description":"Request that execution stop for explicit user approval. The host owns the final decision."}
},
"required":["objective","steps"]
}`)
}

// ReadOnly is true: submitting a plan records a proposal and touches nothing.
func (*SubmitPlanTool) ReadOnly() bool { return true }

// ProviderVisible gates on the host having armed a planning turn, mirroring
// complete_step's phase opt-out: the schema stays constant for cache stability
// and availability is decided when the call runs.
func (*SubmitPlanTool) ProviderVisible(ctx context.Context) bool {
	_, ok := planSubmissionFromContext(ctx)
	return ok
}

func (*SubmitPlanTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	submission, ok := planSubmissionFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("submit_plan is only available while planning; there is no plan to submit in this phase")
	}
	var plan plancontract.Plan
	if err := json.Unmarshal(args, &plan); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	plan = plan.Normalize()
	if err := plan.Validate(); err != nil {
		return "", fmt.Errorf("plan not accepted: %w", err)
	}
	plan = submission.record(plan)
	phases, subSteps := 0, 0
	for _, step := range plan.Steps {
		if step.ParentID == "" {
			phases++
			continue
		}
		subSteps++
	}
	approval := ""
	if plan.RequiresApproval {
		approval = " Approval was requested; the host decides whether execution gates."
	}
	return fmt.Sprintf(
		"Plan revision %d accepted: %d phase(s), %d sub-step(s). The host renders it for the user and hands it to the executor — do not restate it.%s",
		plan.Revision, phases, subSteps, approval), nil
}
