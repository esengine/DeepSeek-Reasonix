// write_fence.go — widening a delegated run's write confinement.
package permission

// ExtendWritePaths is the capability a delegated run asks for when it needs to
// write outside the paths it declared. It is not one of the run's tools: the
// subject is the path being asked for, and the answer decides whether the fence
// moves, not whether one write happens.
const ExtendWritePaths = "extend_write_paths"

// subjectScopedTools are the tools whose authorization reads their subject, so
// a session grant for one must name the subject it was given for: a bare-tool
// grant matches every other subject, and for these that is a different
// decision — one path would open every path, and a low-risk install plan would
// cover a high-risk one. Held by TestEverySubjectSensitiveToolIsSubjectScoped.
var subjectScopedTools = map[string]bool{
	ExtendWritePaths:  true,
	installSourceTool: true,
}

// subjectScopedGrant reports a tool a session grant may not cover by name alone.
func subjectScopedGrant(toolName string) bool {
	return subjectScopedTools[canonicalRuleTool(toolName)]
}

// widensWriteFence reports a request to move a confinement rather than act
// inside one. It always returns to the user whatever the fallback says: "allow
// every write" speaks for this workspace's files, not for redrawing the
// boundary a run was given. An explicit allow rule for the path still wins.
func widensWriteFence(toolName string) bool {
	return canonicalRuleTool(toolName) == ExtendWritePaths
}

// unattendedAsk answers an Ask with no approver attached. Autonomy is a
// statement about a posture with nobody watching; it is not permission to move
// a boundary, so a fence question with no one to answer it is a no — otherwise
// the posture decides the one thing it was never asked about, and YOLO, which
// is built with no approver at all, would grant every widening silently.
func unattendedAsk(toolName string) (bool, string, error) {
	if widensWriteFence(toolName) {
		return false, "widening this run's write paths needs a person, and no approver is attached to this session", nil
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
