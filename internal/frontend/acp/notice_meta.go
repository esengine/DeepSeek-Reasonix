// notice_meta.go — a warning notice as the message chunk ACP clients receive.
package acp

import "reasonix/internal/contract/event"

// warningUpdate is an agent_message_chunk that also names the notice it carries.
type warningUpdate struct {
	SessionUpdate string       `json:"sessionUpdate"`
	Content       ContentBlock `json:"content"`
	Metadata      *struct {
		Notice updateNotice `json:"notice"`
	} `json:"metadata,omitempty"`
}

// updateNotice is a coded notice's stable identity, plus its typed payload
// when the code defines one.
type updateNotice struct {
	Code    string `json:"code"`
	Payload string `json:"payload,omitempty"`
}

func warningChunk(e event.Event) warningUpdate {
	chunk := warningUpdate{
		SessionUpdate: "agent_message_chunk",
		Content:       textBlock("\n\n[warning] " + e.Text),
	}
	if e.Code == "" {
		return chunk
	}
	notice := updateNotice{Code: e.Code}
	if event.DetailIsPayload(e.Code) {
		notice.Payload = e.Detail
	}
	chunk.Metadata = &struct {
		Notice updateNotice `json:"notice"`
	}{Notice: notice}
	return chunk
}
