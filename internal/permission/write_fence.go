// write_fence.go — widening a delegated run's write confinement.
package permission

import (
	"encoding/json"
	"strings"
)

// ExtendWritePaths is the capability a delegated run asks for when it needs to
// write outside the paths it declared. It is not one of the run's tools: the
// subject is the path being asked for, and the answer decides whether the fence
// moves, not whether one write happens.
const ExtendWritePaths = "extend_write_paths"

// subjectSensitiveTools maps each tool whose authorization reads its subject to
// the question it asks of that subject. Membership is one declaration carrying
// three consequences, so none can arrive without the others: the decision
// returns to a person whatever the fallback says, an unattended session refuses
// rather than answering for them, and a grant must name the subject it covers.
var subjectSensitiveTools = map[string]func(subject string) bool{
	ExtendWritePaths:  func(string) bool { return true },
	installSourceTool: func(s string) bool { return strings.HasPrefix(s, selfExtendHumanRisk) },
}

// subjectScopedGrant reports a tool a session grant may not cover by name alone.
func subjectScopedGrant(toolName string) bool {
	_, ok := subjectSensitiveTools[canonicalRuleTool(toolName)]
	return ok
}

// unattendedAsk answers an Ask with no approver attached. Autonomy is a
// statement about a posture with nobody watching; it is not permission to
// decide what only a person may. A question with nobody to answer it is a no —
// otherwise the posture answers for the absent person, and YOLO, built with no
// approver at all, granted every one of these silently.
func unattendedAsk(toolName string, args json.RawMessage) (bool, string, error) {
	if subjectRequiresHuman(toolName, Subject(args)) {
		return false, "this decision needs a person, and no approver is attached to this session", nil
	}
	return true, "", nil
}

// sessionGrantRule is the rule a session grant records for tools that are not
// bash or a file mutation. Approving one of these answered for that subject,
// never for the tool, so the subject stays in the rule.
func sessionGrantRule(toolName, subject string) string {
	if subject != "" && subjectScopedGrant(toolName) {
		return toolName + "=" + subject
	}
	return toolName
}

// SessionGrantMatches reports whether a recorded grant covers this call, and is
// stricter than RuleMatchesString where the tool decides on its subject: a
// bare-tool grant there predates grants carrying subjects and stands for an
// answer nobody gave — one plan was approved, the rule kept the tool. It now
// covers nothing, so the question is asked once more. See ruleNamesSubject.
func SessionGrantMatches(rule, toolName, subject string) bool {
	if subjectScopedGrant(toolName) && !ruleNamesSubject(rule) {
		return false
	}
	return RuleMatchesString(rule, toolName, subject)
}

func ruleNamesSubject(rule string) bool {
	r, ok := ParseRule(rule)
	return ok && strings.TrimSpace(r.Subject) != ""
}
