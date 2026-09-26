// handshake.go — initialize and authenticate: what the agent advertises before a session opens.
package acp

import (
	"context"
	"encoding/json"
	"strings"
)

// initialize advertises the agent's capability set: persisted load plus ACP v1
// list/resume/close/delete lifecycle helpers, prompts carrying images and
// embedded resources but not audio, and stdio / Streamable HTTP MCP (no legacy
// sse).
func (s *service) initialize(_ context.Context, raw json.RawMessage) (any, error) {
	var p InitializeParams
	if len(raw) > 0 && json.Unmarshal(raw, &p) == nil {
		s.setClientCapabilities(p.ClientCapabilities)
	}
	return InitializeResult{
		ProtocolVersion: ProtocolVersion,
		AgentCapabilities: AgentCapabilities{
			LoadSession: true,
			SessionCapabilities: SessionCapabilities{
				List:   &EmptyCapability{},
				Resume: &EmptyCapability{},
				Close:  &EmptyCapability{},
				Delete: &EmptyCapability{},
			},
			PromptCapabilities: PromptCapabilities{
				Image:           true,
				Audio:           false,
				EmbeddedContext: true,
			},
			MCPCapabilities: MCPCapabilities{HTTP: true, SSE: false},
			Meta: goalCapabilities(map[string]any{
				"reasonix.io": ReasonixExtensionCapabilities{
					SessionSteer: &SessionSteerCapability{Method: sessionSteerMethod},
					SessionInbox: &SessionInboxCapability{
						SchemaVersion: sessionInboxSchemaVersion,
						Methods: map[string]string{
							"enqueue":   sessionInboxEnqueueMethod,
							"list":      sessionInboxListMethod,
							"get":       sessionInboxGetMethod,
							"update":    sessionInboxUpdateMethod,
							"delete":    sessionInboxDeleteMethod,
							"move":      sessionInboxMoveMethod,
							"setPaused": sessionInboxPauseMethod,
							"retry":     sessionInboxRetryMethod,
							"refresh":   sessionInboxRefreshMethod,
						},
					},
					SessionReloadExtensions: &SessionReloadExtensionsCapability{Method: sessionReloadExtensionsMethod},
					ExtensionSurface:        &ExtensionSurfaceCapability{Supported: true, SchemaVersion: reasonixExtensionSurfaceSchemaVersion},
				},
				sessionStatusMethod:       ReasonixSchemaCapability{SchemaVersion: reasonixStatusSchemaVersion},
				sessionStatusUpdateMethod: ReasonixSchemaCapability{SchemaVersion: reasonixStatusSchemaVersion},
			}),
		},
		AgentInfo:   Implementation{Name: s.info.Name, Version: s.info.Version},
		AuthMethods: []AuthMethod{reasonixSetupAuthMethod()},
	}, nil
}

func reasonixSetupAuthMethod() AuthMethod {
	return AuthMethod{
		ID:          "reasonix-setup",
		Name:        "Reasonix setup",
		Description: "Configure Reasonix providers and credentials in a terminal",
		Type:        "terminal",
		Args:        []string{"setup"},
	}
}

func (s *service) authenticate(_ context.Context, raw json.RawMessage) (any, error) {
	var p AuthenticateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "authenticate: " + err.Error()}
	}
	if strings.TrimSpace(p.MethodID) != reasonixSetupAuthMethod().ID {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "authenticate: unknown methodId " + p.MethodID}
	}
	return AuthenticateResult{}, nil
}
