package sessionv4

import (
	"encoding/base64"
	"encoding/json"

	"reasonix/internal/contract/provider"
)

// message is one 1.x transcript message: the fields this build shares with
// 1.x, plus the identity and origin 1.x keys it by.
type message struct {
	id  string
	msg provider.Message
}

// v1Extra is what 1.x writes on a message that this build's Message does not
// carry under the same name.
type v1Extra struct {
	ID          string       `json:"id"`
	Origin      string       `json:"origin"`
	ImageInputs []imageInput `json:"image_inputs"`
}

type imageInput struct {
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Attachment *struct {
		Content contentRef `json:"content"`
	} `json:"attachment"`
}

// v1Core is the part of a message every line agrees on; a message whose other
// fields no longer decode here still opens with these.
type v1Core struct {
	Role             provider.Role       `json:"role"`
	Content          string              `json:"content"`
	RawContent       string              `json:"raw_content"`
	ReasoningContent string              `json:"reasoning_content"`
	ToolCalls        []provider.ToolCall `json:"tool_calls"`
	ToolCallID       string              `json:"tool_call_id"`
	Name             string              `json:"name"`
	Images           []string            `json:"images"`
	CreatedAt        int64               `json:"createdAt"`
	LocalOnly        bool                `json:"local_only"`
}

// decodeMessage reads one 1.x message. origin "host" is what this build calls
// HostAuthored, and an image kept in the pool is carried as a data URL.
func decodeMessage(raw json.RawMessage, pool contentPool) (message, error) {
	var extra v1Extra
	if err := json.Unmarshal(raw, &extra); err != nil {
		return message{}, err
	}
	var m provider.Message
	if err := json.Unmarshal(raw, &m); err != nil {
		var core v1Core
		if err := json.Unmarshal(raw, &core); err != nil {
			return message{}, err
		}
		m = provider.Message{
			Role: core.Role, Content: core.Content, RawContent: core.RawContent,
			ReasoningContent: core.ReasoningContent, ToolCalls: core.ToolCalls,
			ToolCallID: core.ToolCallID, Name: core.Name, Images: core.Images,
			CreatedAt: core.CreatedAt, LocalOnly: core.LocalOnly,
		}
	}
	m.HostAuthored = m.HostAuthored || extra.Origin == "host"
	for _, in := range extra.ImageInputs {
		switch {
		case in.Kind == "attachment" && in.Attachment != nil:
			if data, err := pool.read(in.Attachment.Content); err == nil {
				m.Images = append(m.Images, "data:"+in.Attachment.Content.MediaType+";base64,"+base64.StdEncoding.EncodeToString(data))
			}
		case in.Kind == "url" && in.URL != "":
			m.Images = append(m.Images, in.URL)
		}
	}
	return message{id: extra.ID, msg: m}, nil
}
