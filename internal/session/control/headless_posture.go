package control

import "reasonix/internal/contract/config"

// ApplyDefaultHeadlessApprovalMode is ApplyHeadlessApprovalMode for a run nobody
// named a mode for. Where the default asks because this folder is not trusted,
// the refusals say so, with a code and the remedy, instead of "no approver".
func (c *Controller) ApplyDefaultHeadlessApprovalMode() string {
	mode := c.DefaultApprovalMode()
	var folder *folderRefusal
	if trust := c.WorkspaceTrust(); mode == ToolApprovalAsk && c.posture.WritesConfined &&
		TrustableFolder(c.WorkspaceRoot()) && trust != config.WorkspaceTrusted {
		folder = &folderRefusal{root: c.WorkspaceRoot(), declined: trust == config.WorkspaceTrustDeclined}
	}
	c.applyHeadless(mode, folder)
	return mode
}
