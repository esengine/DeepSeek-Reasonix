package main

import "errors"

var (
	errNoActiveTab          = errors.New("no active tab")
	errMCPPromptUnsupported = errors.New("this runtime cannot answer MCP prompts")
)

// AnswerMCPInteraction resolves a pending MCP elicitation for one tab: the
// user's action (accept/decline/cancel) plus, for form accepts, the submitted
// values. Routes through AnswerMCPInteractionChecked so the decision is
// durable before the blocked MCP call resumes.
func (a *App) AnswerMCPInteractionForTab(tabID, id, action string, content map[string]any) error {
	_, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return errNoActiveTab
	}
	resolver, ok := ctrl.(interface {
		AnswerMCPInteractionChecked(id, action string, content map[string]any) error
	})
	if !ok {
		return errMCPPromptUnsupported
	}
	return resolver.AnswerMCPInteractionChecked(id, action, content)
}
