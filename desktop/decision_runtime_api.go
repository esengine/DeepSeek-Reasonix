package main

import (
	"fmt"
	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// PromptAnswerView is the Wails-safe union for all interactive cards.
type PromptAnswerView struct {
	Questions []QuestionAnswer `json:"questions,omitempty"`
	Allow     bool             `json:"allow,omitempty"`
	Session   bool             `json:"session,omitempty"`
	Persist   bool             `json:"persist,omitempty"`
	Action    string           `json:"action,omitempty"`
	Feedback  string           `json:"feedback,omitempty"`
	Content   map[string]any   `json:"content,omitempty"`
}

type PromptIdentityView struct {
	PromptID     string `json:"promptId"`
	TurnID       string `json:"turnId"`
	RuntimeEpoch string `json:"runtimeEpoch,omitempty"`
	Kind         string `json:"kind"`
}

func (a *App) PendingPromptIdentitiesForTab(tabID string) ([]PromptIdentityView, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return []PromptIdentityView{}, a.workspaceNotReadyErr(tab)
	}
	reader, ok := ctrl.(interface {
		PendingPromptIdentities() []control.PromptIdentity
	})
	if !ok {
		return []PromptIdentityView{}, fmt.Errorf("prompt identity replay is unavailable")
	}
	identities := reader.PendingPromptIdentities()
	views := make([]PromptIdentityView, 0, len(identities))
	for _, identity := range identities {
		views = append(views, PromptIdentityView{PromptID: identity.PromptID, TurnID: identity.TurnID, RuntimeEpoch: identity.RuntimeEpoch, Kind: string(identity.Kind)})
	}
	return views, nil
}

// ReplayPendingPromptIdentitiesForTab returns the authoritative identity list
// without re-emitting cards. The existing ReplayPendingPromptsForTab method
// remains the event replay compatibility surface.
func (a *App) ReplayPendingPromptIdentitiesForTab(tabID string) ([]PromptIdentityView, error) {
	return a.PendingPromptIdentitiesForTab(tabID)
}

// ResolvePromptForTab is the canonical exact-identity decision endpoint.
func (a *App) ResolvePromptForTab(tabID, promptID, turnID, runtimeEpoch, kind string, answer PromptAnswerView) error {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return a.workspaceNotReadyErr(tab)
	}
	questions := make([]event.AskAnswer, len(answer.Questions))
	for i, q := range answer.Questions {
		questions[i] = event.AskAnswer{QuestionID: q.QuestionID, Selected: q.Selected}
	}
	resolver, ok := ctrl.(interface {
		ResolvePromptExact(control.PromptIdentity, control.PromptAnswer) error
	})
	if !ok {
		return fmt.Errorf("this runtime cannot resolve exact prompts")
	}
	return resolver.ResolvePromptExact(
		control.PromptIdentity{PromptID: promptID, TurnID: turnID, RuntimeEpoch: runtimeEpoch, Kind: control.PromptKind(kind)},
		control.PromptAnswer{Questions: questions, Allow: answer.Allow, Session: answer.Session, Persist: answer.Persist, Action: answer.Action, Feedback: answer.Feedback, Content: answer.Content},
	)
}

// ApproveTabForTurn is the exact-identity approval entry point for new
// frontends. The legacy ApproveTab method remains available to old clients.
func (a *App) ApproveTabForTurn(tabID, turnID, runtimeEpoch, id string, allow, session, persist bool) error {
	ctrl, err := a.validatePromptIdentity(tabID, turnID, runtimeEpoch)
	if err != nil {
		return err
	}
	ctrl.Approve(id, allow, session, persist)
	return nil
}

func (a *App) ResolvePlanDecisionTabForTurn(tabID, turnID, runtimeEpoch, id, action string) error {
	ctrl, err := a.validatePromptIdentity(tabID, turnID, runtimeEpoch)
	if err != nil {
		return err
	}
	return ctrl.ResolvePlanDecision(id, control.PlanDecisionAction(action))
}

func (a *App) ResolveRecoveryTabForTurn(tabID, turnID, runtimeEpoch, id, action, feedback string) error {
	ctrl, err := a.validatePromptIdentity(tabID, turnID, runtimeEpoch)
	if err != nil {
		return err
	}
	return ctrl.ResolveRecovery(id, agent.RecoveryAction(action), feedback)
}

func (a *App) AnswerMCPInteractionForTurn(tabID, turnID, runtimeEpoch, id, action string, content map[string]any) error {
	ctrl, err := a.validatePromptIdentity(tabID, turnID, runtimeEpoch)
	if err != nil {
		return err
	}
	resolver, ok := ctrl.(interface {
		AnswerMCPInteractionChecked(string, string, map[string]any) error
	})
	if !ok {
		return errMCPPromptUnsupported
	}
	return resolver.AnswerMCPInteractionChecked(id, action, content)
}
