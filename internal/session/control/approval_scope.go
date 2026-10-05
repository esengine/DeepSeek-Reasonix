package control

import "reasonix/internal/contract/tool"

// approvalScope is what approving a call to name authorizes beyond the call,
// as the tool declares it; see tool.ApprovalScoper.
func (c *Controller) approvalScope(name string) string {
	reg := c.mcp.registry()
	if reg == nil {
		return ""
	}
	t, ok := reg.Get(name)
	if !ok {
		return ""
	}
	return tool.ApprovalScopeOf(t)
}
