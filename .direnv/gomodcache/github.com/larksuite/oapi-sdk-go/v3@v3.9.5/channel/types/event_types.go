package types

// ReactionEvent represents a reaction added or removed from a message.
type ReactionEvent struct {
	EventID      string      `json:"event_id"`
	MessageID    string      `json:"message_id"`
	ReactionType string      `json:"reaction_type"` // e.g. "SMILE"
	OperatorType string      `json:"operator_type,omitempty"`
	UserID       string      `json:"user_id"`
	Action       string      `json:"action"` // "add" or "remove"
	CreateTimeMs int64       `json:"create_time_ms"`
	RawEvent     interface{} `json:"raw_event"`
}

// OperatorInfo represents the user who triggered the event.
type OperatorInfo struct {
	OpenID  string `json:"open_id"`
	UserID  string `json:"user_id,omitempty"`
	UnionID string `json:"union_id,omitempty"`
}

// CommentEvent represents a comment/reply on a drive document.
type CommentEvent struct {
	EventID      string       `json:"event_id"`
	CommentID    string       `json:"comment_id"`
	FileToken    string       `json:"file_token"`
	FileType     string       `json:"file_type"`
	Operator     OperatorInfo `json:"operator"`
	ReplyID      string       `json:"reply_id"`
	MentionedBot bool         `json:"mentioned_bot"`
	Timestamp    int64        `json:"timestamp"`
	RawEvent     interface{}  `json:"raw_event"`
}

// BotAddedEvent represents an event when the bot is added to a chat.
type BotAddedEvent struct {
	EventID      string      `json:"event_id"`
	ChatID       string      `json:"chat_id"`
	ChatName     string      `json:"chat_name"`
	UserID       string      `json:"user_id"` // User who added the bot
	CreateTimeMs int64       `json:"create_time_ms"`
	RawEvent     interface{} `json:"raw_event"`
}

// CardActionEvent represents an interactive card action callback.
type CardActionEvent struct {
	EventID      string             `json:"event_id"`
	MessageID    string             `json:"message_id"`
	ChatID       string             `json:"chat_id"`
	Token        string             `json:"token,omitempty"`
	Host         string             `json:"host,omitempty"`
	DeliveryType string             `json:"delivery_type,omitempty"`
	Operator     CardActionOperator `json:"operator"`
	Action       CardActionPayload  `json:"action"`
	Context      CardActionContext  `json:"context"`
	RawEvent     interface{}        `json:"raw_event"`
}

type CardActionOperator struct {
	OpenID    string `json:"open_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	TenantKey string `json:"tenant_key,omitempty"`
}

type CardActionPayload struct {
	Value      map[string]interface{} `json:"value,omitempty"`
	Tag        string                 `json:"tag,omitempty"`
	Option     string                 `json:"option,omitempty"`
	Timezone   string                 `json:"timezone,omitempty"`
	Name       string                 `json:"name,omitempty"`
	FormValue  map[string]interface{} `json:"form_value,omitempty"`
	InputValue string                 `json:"input_value,omitempty"`
	Options    []string               `json:"options,omitempty"`
	Checked    bool                   `json:"checked,omitempty"`
}

type CardActionContext struct {
	URL           string `json:"url,omitempty"`
	PreviewToken  string `json:"preview_token,omitempty"`
	OpenMessageID string `json:"open_message_id,omitempty"`
	OpenChatID    string `json:"open_chat_id,omitempty"`
}

// RejectEvent represents a message rejected by safety policies.
type RejectEvent struct {
	MessageID string `json:"message_id"`
	ChatID    string `json:"chat_id"`
	SenderID  string `json:"sender_id"`
	Reason    string `json:"reason"`
}
