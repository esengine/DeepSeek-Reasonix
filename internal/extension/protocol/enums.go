package protocol

import "sort"

type Direction string

const (
	// DirectionHostToExtensionRequest is a Host → Extension request; the
	// extension answers with the registered result DTO.
	DirectionHostToExtensionRequest Direction = "host_to_extension_request"
	// DirectionExtensionToHostRequest is an Extension → Host request; the
	// host answers with the registered result DTO.
	DirectionExtensionToHostRequest Direction = "extension_to_host_request"
	// DirectionHostToExtensionNotification is a fire-and-forget Host →
	// Extension notification.
	DirectionHostToExtensionNotification Direction = "host_to_extension_notification"
	// DirectionExtensionToHostNotification is a fire-and-forget Extension →
	// Host notification (provider stream chunks).
	DirectionExtensionToHostNotification Direction = "extension_to_host_notification"
)

// IsNotification reports whether the direction carries no response.
func (d Direction) IsNotification() bool {
	return d == DirectionHostToExtensionNotification || d == DirectionExtensionToHostNotification
}

type OperationClass string

const (
	ClassLifecycle   OperationClass = "lifecycle"
	ClassIntercept   OperationClass = "intercept"
	ClassObservation OperationClass = "observation"
	ClassProvider    OperationClass = "provider"
	ClassUI          OperationClass = "ui"
	ClassContent     OperationClass = "content"
)

// InterceptEvent names one of the 17 frozen kernel hook points an extension
// may intercept (extension/intercept) or observe (extension/event). The
// string values mirror internal/extension.InterceptorPoint exactly; they are
// frozen here so the public wire contract does not depend on kernel
// internals.
type InterceptEvent string

const (
	EventSessionStart       InterceptEvent = "session.start"
	EventSessionEnd         InterceptEvent = "session.end"
	EventSessionLoad        InterceptEvent = "session.load"
	EventSessionSave        InterceptEvent = "session.save"
	EventSessionRotate      InterceptEvent = "session.rotate"
	EventInputReceive       InterceptEvent = "input.receive"
	EventAgentBeforeStart   InterceptEvent = "agent.before_start"
	EventSystemPromptBuild  InterceptEvent = "system_prompt.build"
	EventContextPrepare     InterceptEvent = "context.prepare"
	EventProviderRequest    InterceptEvent = "provider.request"
	EventProviderResponse   InterceptEvent = "provider.response"
	EventToolBefore         InterceptEvent = "tool.before"
	EventToolAfter          InterceptEvent = "tool.after"
	EventPermissionDecision InterceptEvent = "permission.decision"
	EventCompactionPrepare  InterceptEvent = "compaction.prepare"
	EventCompactionComplete InterceptEvent = "compaction.complete"
	EventFrontendEvent      InterceptEvent = "frontend.event"
)

// InterceptEvents returns the 17 frozen hook point names, sorted. Adding an
// event is a conscious protocol change: the count is pinned by tests and the
// list is frozen into the generated schema document.
func InterceptEvents() []string {
	out := []string{
		string(EventSessionStart), string(EventSessionEnd), string(EventSessionLoad),
		string(EventSessionSave), string(EventSessionRotate), string(EventInputReceive),
		string(EventAgentBeforeStart), string(EventSystemPromptBuild), string(EventContextPrepare),
		string(EventProviderRequest), string(EventProviderResponse), string(EventToolBefore),
		string(EventToolAfter), string(EventPermissionDecision), string(EventCompactionPrepare),
		string(EventCompactionComplete), string(EventFrontendEvent),
	}
	sort.Strings(out)
	return out
}

// InterceptDecision is the extension's ruling on an intercepted event.
type InterceptDecision string

const (
	DecisionContinue InterceptDecision = "continue"
	DecisionBlock    InterceptDecision = "block"
	DecisionReplace  InterceptDecision = "replace"
	DecisionAllow    InterceptDecision = "allow"
	DecisionDeny     InterceptDecision = "deny"
)

// UIHostKind identifies which host UI surface family renders extension UI.
type UIHostKind string

const (
	UIHostTUI      UIHostKind = "tui"
	UIHostDesktop  UIHostKind = "desktop"
	UIHostACP      UIHostKind = "acp"
	UIHostHeadless UIHostKind = "headless"
)

// UISurfaceKind is the kind of structured surface an extension publishes.
type UISurfaceKind string

const (
	UISurfaceStatus       UISurfaceKind = "status"
	UISurfaceCard         UISurfaceKind = "card"
	UISurfaceForm         UISurfaceKind = "form"
	UISurfaceNotification UISurfaceKind = "notification"
)

// UIRequestKind is the kind of blocking UI prompt the host shows on an
// extension's behalf.
type UIRequestKind string

const (
	UIRequestConfirm     UIRequestKind = "confirm"
	UIRequestInput       UIRequestKind = "input"
	UIRequestSelect      UIRequestKind = "select"
	UIRequestMultiselect UIRequestKind = "multiselect"
)

// UIFieldKind is the input kind of one form field. Values mirror
// UIRequestKind deliberately: a form composes the same primitive prompts.
type UIFieldKind string

const (
	UIFieldConfirm     UIFieldKind = "confirm"
	UIFieldInput       UIFieldKind = "input"
	UIFieldSelect      UIFieldKind = "select"
	UIFieldMultiselect UIFieldKind = "multiselect"
)

// UISeverity grades status and notification payloads.
type UISeverity string

const (
	UISeverityInfo  UISeverity = "info"
	UISeverityWarn  UISeverity = "warn"
	UISeverityError UISeverity = "error"
)

// ProviderRole mirrors the provider message roles without importing
// internal/provider into the public wire schema.
type ProviderRole string

const (
	ProviderRoleSystem    ProviderRole = "system"
	ProviderRoleUser      ProviderRole = "user"
	ProviderRoleAssistant ProviderRole = "assistant"
	ProviderRoleTool      ProviderRole = "tool"
)

// ProviderChunkType classifies one provider stream chunk.
type ProviderChunkType string

const (
	ChunkText          ProviderChunkType = "text"
	ChunkReasoning     ProviderChunkType = "reasoning"
	ChunkToolCallStart ProviderChunkType = "tool_call_start"
	ChunkToolCallDelta ProviderChunkType = "tool_call_args_delta"
	ChunkToolCall      ProviderChunkType = "tool_call"
	ChunkUsage         ProviderChunkType = "usage"
	ChunkDone          ProviderChunkType = "done"
	ChunkError         ProviderChunkType = "error"
)

// ProviderErrorCode classifies redacted provider stream failures. Raw
// provider errors may contain credentials or endpoints and never cross the
// wire; only these codes and generic messages do.
type ProviderErrorCode string

const (
	ProviderFailed      ProviderErrorCode = "provider_failed"
	ProviderInterrupted ProviderErrorCode = "provider_interrupted"
)

// ContentEncoding names the canonical text encoding of content ref data.
type ContentEncoding string

const ContentUTF8 ContentEncoding = "utf8"
