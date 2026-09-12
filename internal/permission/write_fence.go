// write_fence.go — widening a delegated run's write confinement.
package permission

// ExtendWritePaths is the capability a delegated run asks for when it needs to
// write outside the paths it declared. It is not one of the run's tools: the
// subject is the path being asked for, and the answer decides whether the fence
// moves, not whether one write happens.
const ExtendWritePaths = "extend_write_paths"

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
