package agent

// Legacy text-plan fallback: reading a planner's prose for decisions a
// submitted plan states in a field. It retires as a unit once both plan
// producers submit structured plans.

import "strings"

// plannerApprovalPhrases is the fallback for planners that ignore the
// structured marker. Claims of past approval ("用户已批准", "already approved")
// are deliberately included: the planner cannot know host approval state, so a
// claimed approval is re-gated instead of trusted.
var plannerApprovalPhrases = []string{
	"是否批准",
	"等待用户批准",
	"等待您的批准",
	"待用户批准",
	"批准这个方案",
	"批准该方案",
	"批准此方案",
	"批准这个计划",
	"批准该计划",
	"批准此计划",
	"批准方案后",
	"批准计划后",
	"用户已批准",
	"用户已经批准",
	"已经获得批准",
	"approve this plan",
	"approve the plan",
	"approval before",
	"waiting for approval",
	"awaiting approval",
	"wait for user approval",
	"user approved",
	"already approved",
	"has approved",
}

func plannerPlanRequestsApproval(plan string) bool {
	lower := strings.ToLower(strings.TrimSpace(plan))
	if lower == "" {
		return false
	}
	if strings.ToLower(lastNonEmptyLine(lower)) == plannerRequiresApprovalMarker {
		return true
	}
	// Match per line so a nearby negation ("无需等待用户批准", "no need to wait
	// for approval") exempts only its own phrase, not the whole plan.
	for rawLine := range strings.SplitSeq(lower, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		for _, phrase := range plannerApprovalPhrases {
			before, _, ok := strings.Cut(line, phrase)
			if !ok {
				continue
			}
			if approvalMentionNegated(before) {
				continue
			}
			return true
		}
	}
	return false
}

// approvalMentionNegated reports whether the text just before a matched phrase
// negates it, so a plan that rules out an approval round ("无需等待用户批准")
// does not trigger one. Only the nearby prefix counts, and erring toward
// gating is fine: the failure mode is an extra prompt, never a silent run.
func approvalMentionNegated(prefix string) bool {
	const window = 30
	if len(prefix) > window {
		prefix = prefix[len(prefix)-window:]
	}
	for _, neg := range []string{"无需", "无须", "不需要", "不需", "不必", "不用", "no need", "not require", "not required", "without"} {
		if strings.Contains(prefix, neg) {
			return true
		}
	}
	return false
}
