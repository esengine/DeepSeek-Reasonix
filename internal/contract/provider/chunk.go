package provider

import "encoding/json"

// ChunkType identifies the kind of a streamed increment.
type ChunkType int

const (
	ChunkText              ChunkType = iota // text delta
	ChunkReasoning                          // thinking-mode reasoning delta (before the visible answer)
	ChunkToolCallStart                      // a tool call has begun (ToolCall: ID+Name; args still streaming)
	ChunkToolCallArgsDelta                  // progress while a call's arguments stream (ToolCall: ID+Name; ArgChars: cumulative)
	ChunkToolCall                           // one complete tool call
	ChunkUsage                              // token usage for the completion
	ChunkDone                               // completion finished normally
	ChunkError                              // an error occurred
	ChunkResponsesItem                      // a complete provider-issued Responses API output item for stateless replay
	ChunkProviderTool                       // a tool the provider ran server-side, already finished (ToolCall: ID/Name/Arguments; Text: result)
)

// Chunk is a single streamed event. Read the field matching Type.
type Chunk struct {
	Type      ChunkType
	Text      string // ChunkText, ChunkReasoning, ChunkProviderTool (the result)
	Signature string // ChunkReasoning: opaque proof for the reasoning (Anthropic thinking signature), when issued
	// ReasoningID/ReasoningStatus ride the final ChunkReasoning of a turn (empty
	// Text) so the Agent can persist them and round-trip the next turn's input
	// reasoning item (review #7234 — Responses marks Reasoning.id required).
	ReasoningID     string          // ChunkReasoning: provider-issued reasoning item id
	ReasoningStatus string          // ChunkReasoning: final reasoning item status ("completed")
	ToolCall        *ToolCall       // ChunkToolCallStart (ID+Name only), ChunkToolCallArgsDelta (ID+Name), ChunkToolCall (complete)
	ArgChars        int             // ChunkToolCallArgsDelta: cumulative argument characters received for this call
	ResponsesItem   json.RawMessage // ChunkResponsesItem: opaque validated Responses API output item
	Usage           *Usage          // ChunkUsage
	Err             error           // ChunkError
}
