package acp

import (
	"context"
	"encoding/json"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/mcpinteraction"
)

const mcpInteractionMethod = "_reasonix.io/mcp/request_interaction"

func (s *updateSink) bindControllerPrompts(ctrl *control.Controller, interactions bool) {
	s.bindApprove(ctrl.Approve)
	s.bindAnswer(ctrl.AnswerQuestion)
	s.bindMCPInteraction(interactions, ctrl.AnswerMCPInteractionChecked)
}

func (s *updateSink) emitPrompt(e event.Event) {
	// Requests run off the synchronous event sink so user decisions can release
	// the blocked controller. Each request remains bound to its originating turn.
	switch e.Kind {
	case event.ApprovalRequest:
		go s.requestPermission(s.currentTurnContext(), e.Approval)
	case event.AskRequest:
		go s.requestAsk(s.currentTurnContext(), e.Ask)
	case event.MCPInteractionRequest:
		s.emitMCPInteraction(e.MCPInteraction)
	}
}

// MCPInteractionCapability is negotiated explicitly by capable ACP clients.
// Older clients retain the core MCP profile and receive no reverse requests.
type MCPInteractionCapability struct {
	Supported     bool   `json:"supported"`
	SchemaVersion int    `json:"schemaVersion"`
	Method        string `json:"method,omitempty"`
}

type MCPInteractionParams struct {
	SessionID       string          `json:"sessionId"`
	PromptID        string          `json:"promptId"`
	TurnID          string          `json:"turnId"`
	Server          string          `json:"server"`
	Mode            string          `json:"mode"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	URL             string          `json:"url,omitempty"`
	ElicitationID   string          `json:"elicitationId,omitempty"`
}

type MCPInteractionResult struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

func clientMCPInteractionSupported(caps ClientCapabilities) bool {
	vendor, ok := caps.Meta["reasonix.io"].(map[string]any)
	if !ok {
		return false
	}
	raw, err := json.Marshal(vendor["mcpInteraction"])
	if err != nil {
		return false
	}
	var capability MCPInteractionCapability
	return json.Unmarshal(raw, &capability) == nil && capability.Supported && capability.SchemaVersion == 1
}

func (s *updateSink) bindMCPInteraction(supported bool, answer func(string, string, map[string]any) error) {
	s.mu.Lock()
	s.mcpInteractionSupported, s.answerMCPInteraction = supported, answer
	s.mu.Unlock()
}

func (s *updateSink) emitMCPInteraction(req event.MCPInteraction) {
	s.mu.Lock()
	supported, answer, ctx := s.mcpInteractionSupported, s.answerMCPInteraction, s.turnCtx
	s.mu.Unlock()
	if answer == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Capture the originating controller callback before dispatch. A late reply
	// must never resolve a numerically identical prompt on a replacement controller.
	go func() {
		result := MCPInteractionResult{Action: mcpinteraction.ActionCancel}
		if supported && ctx.Err() == nil && mcpinteraction.SanitizeURLMode(mcpinteraction.Request{Mode: req.Mode, URL: req.URL}) {
			params := MCPInteractionParams{SessionID: s.sessionID, PromptID: req.ID, TurnID: req.TurnID,
				Server: req.Server, Mode: req.Mode, Message: req.Message, RequestedSchema: req.RequestedSchema,
				URL: req.URL, ElicitationID: req.ElicitationID}
			raw, err := s.conn.Request(ctx, mcpInteractionMethod, params)
			var response MCPInteractionResult
			if err == nil && ctx.Err() == nil && json.Unmarshal(raw, &response) == nil {
				switch response.Action {
				case mcpinteraction.ActionAccept, mcpinteraction.ActionDecline, mcpinteraction.ActionCancel:
					result = response
				}
			}
		}
		if result.Action != mcpinteraction.ActionAccept {
			result.Content = nil
		}
		// The controller persists the decision before releasing the MCP waiter;
		// a persistence error leaves the owning turn failed, never auto-approved.
		_ = answer(req.ID, result.Action, result.Content)
	}()
}
