// Package acp implements the Agent Client Protocol (https://agentclientprotocol.com)
// transport: a stdio JSON-RPC 2.0 agent that editors and other host clients speak
// to drive Reasonix. Many tools integrated with the v1 (main-branch) agent over
// ACP, so v2 keeps the wire contract identical — the wire types in this file are a
// faithful port of main's src/acp/protocol.ts (ACP protocol version 1).
//
// The package is an adapter layer over the v2 kernel and depends only on stable
// contracts: it maps the agent's typed event.Event stream onto session/update
// notifications (see dispatch.go), bridges permission.Approver onto
// session/request_permission round-trips (see permission.go), and exposes the
// whole thing over NDJSON JSON-RPC (see server.go). How a per-session agent is
// actually assembled — provider, tools rooted at the session cwd, per-session MCP
// — is left to a Factory the composition root supplies (see service.go), so this
// package stays independent of the cli wiring.
package acp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProtocolVersion is the ACP version this agent implements. Matches main.
const ProtocolVersion = 1

// JSON-RPC 2.0 error codes (subset used on the wire). Mirrors protocol.ts.
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
)

// --- initialize ---

// InitializeParams is the client's handshake. We accept and ignore its
// capabilities/info — the agent advertises a fixed capability set in reply.
type InitializeParams struct {
	ProtocolVersion int             `json:"protocolVersion"`
	ClientInfo      *Implementation `json:"clientInfo,omitempty"`
}

// Implementation names a participant (client or agent) on the wire.
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// InitializeResult advertises what this agent supports: persisted session load,
// ACP v1 session lifecycle helpers, inline resource text (embeddedContext) but
// not image/audio, and stdio / Streamable HTTP MCP (no legacy sse).
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         Implementation    `json:"agentInfo"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

// AgentCapabilities is the agentCapabilities object in InitializeResult.
type AgentCapabilities struct {
	LoadSession         bool                `json:"loadSession"`
	SessionCapabilities SessionCapabilities `json:"sessionCapabilities,omitempty"`
	PromptCapabilities  PromptCapabilities  `json:"promptCapabilities"`
	MCPCapabilities     MCPCapabilities     `json:"mcpCapabilities"`
}

// EmptyCapability serializes to {} for ACP capability flags.
type EmptyCapability struct{}

// SessionCapabilities advertises optional session lifecycle methods.
type SessionCapabilities struct {
	List   *EmptyCapability `json:"list,omitempty"`
	Resume *EmptyCapability `json:"resume,omitempty"`
	Close  *EmptyCapability `json:"close,omitempty"`
	Delete *EmptyCapability `json:"delete,omitempty"`
}

// PromptCapabilities reports which content-block kinds prompts may carry.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// MCPCapabilities reports which MCP transports session/new accepts.
type MCPCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AuthMethod advertises how a client can prepare credentials for the agent.
type AuthMethod struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Type        string            `json:"type,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// AuthenticateParams selects one advertised auth method. Terminal methods are
// normally handled by the client by launching the agent with the method's args;
// accepting this request keeps clients that call authenticate directly working.
type AuthenticateParams struct {
	MethodID string `json:"methodId"`
}

// AuthenticateResult is the empty authentication ack.
type AuthenticateResult struct{}

// --- session/new ---

// SessionNewParams opens a session rooted at cwd, optionally with MCP servers
// the agent should connect for the session's lifetime.
type SessionNewParams struct {
	Cwd        string          `json:"cwd,omitempty"`
	MCPServers []MCPServerSpec `json:"mcpServers,omitempty"`
}

// MCPServerSpec describes one MCP server the client asks the agent to connect.
type MCPServerSpec struct {
	Name    string     `json:"name"`
	Type    string     `json:"type,omitempty"`
	Command string     `json:"command,omitempty"`
	Args    []string   `json:"args,omitempty"`
	Env     MCPEnv     `json:"env,omitempty"`
	URL     string     `json:"url,omitempty"`
	Headers MCPHeaders `json:"headers,omitempty"`
}

// MCPEnv accepts ACP's official EnvVariable[] shape while still accepting the
// older map shape that Reasonix v1 clients used.
type MCPEnv map[string]string

// MCPHeaders accepts ACP's official HTTPHeader[] shape while still accepting
// the older map shape that Reasonix v1 clients used. The official spec
// (https://agentclientprotocol.com) ships HTTP/SSE MCP headers as an array of
// {name,value} objects, even when empty.
type MCPHeaders map[string]string

// EnvVariable is one official ACP MCP environment variable entry. The same
// {name,value} shape is also used by HTTP/SSE headers in the ACP spec, so we
// reuse it as the parse target for [MCPHeaders] too.
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (e *MCPEnv) UnmarshalJSON(raw []byte) error {
	out, err := unmarshalNameValueMap(raw, "env")
	if err != nil {
		return err
	}
	*e = out
	return nil
}

func (h *MCPHeaders) UnmarshalJSON(raw []byte) error {
	out, err := unmarshalNameValueMap(raw, "headers")
	if err != nil {
		return err
	}
	*h = out
	return nil
}

// unmarshalNameValueMap parses ACP's official [{name,value}, ...] array shape
// or the legacy {name: value, ...} map shape into a map. field names the JSON
// field for error messages.
func unmarshalNameValueMap(raw []byte, field string) (map[string]string, error) {
	if s := strings.TrimSpace(string(raw)); s == "" || s == "null" {
		return nil, nil
	}

	var vars []EnvVariable
	if err := json.Unmarshal(raw, &vars); err == nil {
		out := make(map[string]string, len(vars))
		for i, v := range vars {
			if strings.TrimSpace(v.Name) == "" {
				return nil, fmt.Errorf("%s[%d].name is required", field, i)
			}
			out[v.Name] = v.Value
		}
		return out, nil
	}

	var legacy map[string]string
	if err := json.Unmarshal(raw, &legacy); err == nil {
		return legacy, nil
	}
	return nil, fmt.Errorf("%s must be an array of {name,value} objects", field)
}

// SessionNewResult returns the opaque id used to address the session thereafter.
type SessionNewResult struct {
	SessionID     string                `json:"sessionId"`
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// ModelInfo describes one selectable model in ACP's legacy model selector.
type ModelInfo struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionModelState is ACP's legacy model selector state. New clients should
// prefer the category:"model" config option, but some hosts still probe this.
type SessionModelState struct {
	AvailableModels []ModelInfo `json:"availableModels"`
	CurrentModelID  string      `json:"currentModelId"`
}

// --- session/load ---

// SessionLoadParams resumes a session saved under sessionId (the id a prior
// session/new returned), optionally re-rooting it at cwd with fresh MCP servers.
// The agent replays the stored conversation as session/update notifications
// before the request returns.
type SessionLoadParams struct {
	SessionID  string          `json:"sessionId"`
	Cwd        string          `json:"cwd,omitempty"`
	MCPServers []MCPServerSpec `json:"mcpServers,omitempty"`
}

// SessionLoadResult is the empty ack; the conversation has already arrived as a
// burst of session/update notifications by the time it is sent.
type SessionLoadResult struct {
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// --- session/resume ---

// SessionResumeParams resumes a session without replaying its transcript.
type SessionResumeParams struct {
	SessionID  string          `json:"sessionId"`
	Cwd        string          `json:"cwd,omitempty"`
	MCPServers []MCPServerSpec `json:"mcpServers,omitempty"`
}

// SessionResumeResult is the empty ack returned once the session is ready.
type SessionResumeResult struct {
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// --- session/set_config_option ---

// SetSessionConfigOptionParams changes one advertised session config option.
type SetSessionConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetSessionConfigOptionResult returns the full refreshed config state.
type SetSessionConfigOptionResult struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// SessionConfigOption is a single-value ACP session selector.
type SessionConfigOption struct {
	ID           string                      `json:"id"`
	Name         string                      `json:"name"`
	Description  string                      `json:"description,omitempty"`
	Category     string                      `json:"category,omitempty"`
	Type         string                      `json:"type"`
	CurrentValue string                      `json:"currentValue"`
	Options      []SessionConfigSelectOption `json:"options"`
}

// SessionConfigSelectOption is one selectable value for a config option.
type SessionConfigSelectOption struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// --- session/set_model ---

// SetSessionModelParams is ACP's legacy model-switching request.
type SetSessionModelParams struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

// SetSessionModelResult is the empty ack for legacy model switching.
type SetSessionModelResult struct{}

// --- session/list ---

// SessionListParams lists known sessions, optionally filtered by cwd.
type SessionListParams struct {
	Cwd    string `json:"cwd,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// SessionListResult is the first and only page of sessions Reasonix currently
// returns. NextCursor is omitted because the in-process list is unpaged.
type SessionListResult struct {
	Sessions   []SessionInfo `json:"sessions"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// SessionInfo is the ACP session/list item shape.
type SessionInfo struct {
	SessionID string         `json:"sessionId"`
	Cwd       string         `json:"cwd"`
	Title     string         `json:"title,omitempty"`
	UpdatedAt string         `json:"updatedAt,omitempty"`
	Meta      map[string]any `json:"_meta,omitempty"`
}

// --- session/close ---

// SessionCloseParams closes an active session and releases its resources.
type SessionCloseParams struct {
	SessionID string `json:"sessionId"`
}

// SessionCloseResult is the empty close ack.
type SessionCloseResult struct{}

// --- session/delete ---

// SessionDeleteParams removes a session from future session/list results.
type SessionDeleteParams struct {
	SessionID string `json:"sessionId"`
}

// SessionDeleteResult is the empty delete ack.
type SessionDeleteResult struct{}

// --- content blocks (inbound prompt) ---

// ContentBlock is one piece of a prompt. The agent reads text blocks and the
// inline text of resource blocks (embeddedContext); image/audio are accepted on
// the wire but ignored, matching the advertised capabilities.
type ContentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Resource *ResourceContents `json:"resource,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	Data     string            `json:"data,omitempty"`
}

// ResourceContents is the embedded resource of a "resource" content block.
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// FlattenPrompt extracts the user-visible prompt text out of ACP content blocks.
// Text blocks contribute their text; resource blocks contribute their inline
// text when present (embeddedContext). Other block kinds are dropped. Ported from
// protocol.ts flattenPrompt.
func FlattenPrompt(blocks []ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "resource":
			if b.Resource != nil && b.Resource.Text != "" {
				parts = append(parts, b.Resource.Text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// --- session/prompt ---

// SessionPromptParams sends a turn's prompt to a session.
type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// StopReason tells the client why a turn ended. Values match main's wire.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopCancelled StopReason = "cancelled"
	StopError     StopReason = "error"
)

// SessionPromptResult ends a session/prompt. TranscriptPath is reserved for a
// future on-disk transcript pointer; omitted (null) for now.
type SessionPromptResult struct {
	StopReason     StopReason `json:"stopReason"`
	TranscriptPath *string    `json:"transcriptPath,omitempty"`
}

// --- session/update (agent → client notifications) ---
//
// SessionUpdate is a tagged union discriminated by sessionUpdate. The variants
// reuse the JSON key "content" with two incompatible shapes (a single block for
// message chunks, an array for tool results), so we model each variant as its own
// struct rather than one struct with conflicting tags, and carry it through
// SessionUpdateParams.Update as an interface value.

// SessionUpdateParams wraps one update for a session.
type SessionUpdateParams struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

// messageChunk is agent_message_chunk / agent_thought_chunk.
type messageChunk struct {
	SessionUpdate string       `json:"sessionUpdate"`
	Content       ContentBlock `json:"content"`
	Metadata      *updateMeta  `json:"metadata,omitempty"`
}

// updateMeta carries optional error detail on an agent_message_chunk.
type updateMeta struct {
	Error *updateError `json:"error,omitempty"`
}

type updateError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// toolCall is a "tool_call" update (announces a call, with title/kind/rawInput).
type toolCall struct {
	SessionUpdate string          `json:"sessionUpdate"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	Status        string          `json:"status,omitempty"`
	RawInput      json.RawMessage `json:"rawInput,omitempty"`
}

// toolCallUpdateMsg is a "tool_call_update" update (status + result content).
type toolCallUpdateMsg struct {
	SessionUpdate string        `json:"sessionUpdate"`
	ToolCallID    string        `json:"toolCallId"`
	Status        string        `json:"status,omitempty"`
	Content       []toolContent `json:"content,omitempty"`
}

// toolContent wraps a tool result's text, per the ACP tool_call_update shape.
type toolContent struct {
	Type    string       `json:"type"`
	Content ContentBlock `json:"content"`
}

// availableCommandsUpdate advertises slash commands that the ACP client may
// surface in its composer. The client sends invocations back as normal
// session/prompt text such as "/review diff".
type availableCommandsUpdate struct {
	SessionUpdate     string             `json:"sessionUpdate"`
	AvailableCommands []AvailableCommand `json:"availableCommands"`
}

// AvailableCommand is one slash command available in a session.
type AvailableCommand struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

// AvailableCommandInput describes a command's free-form text argument.
type AvailableCommandInput struct {
	Hint string `json:"hint"`
}

// configOptionUpdate reports a complete refreshed session config state.
type configOptionUpdate struct {
	SessionUpdate string                `json:"sessionUpdate"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// --- session/cancel (client → agent notification) ---

// SessionCancelParams cancels an in-progress turn.
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// --- session/request_permission (agent → client request) ---

// PermissionOptionKind classifies an option for host UI styling. It is an ACP v1
// wire enum, so host-visible permission choices must stay within the official
// protocol values.
type PermissionOptionKind string

const (
	OptAllowOnce    PermissionOptionKind = "allow_once"
	OptAllowAlways  PermissionOptionKind = "allow_always"
	OptRejectOnce   PermissionOptionKind = "reject_once"
	OptRejectAlways PermissionOptionKind = "reject_always"
)

// PermissionOption is one choice offered to the user for a permission request.
type PermissionOption struct {
	OptionID string               `json:"optionId"`
	Name     string               `json:"name"`
	Kind     PermissionOptionKind `json:"kind"`
}

// PermissionRequestParams asks the client to approve a pending tool call.
type PermissionRequestParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  PermissionToolCall `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// PermissionToolCall describes the call awaiting approval.
type PermissionToolCall struct {
	ToolCallID string          `json:"toolCallId"`
	Title      string          `json:"title,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Status     string          `json:"status,omitempty"`
	Content    []toolContent   `json:"content,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
}

// PermissionRequestResult is the client's reply to a permission request.
type PermissionRequestResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// PermissionOutcome is "selected" (with optionId) or "cancelled".
type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}
