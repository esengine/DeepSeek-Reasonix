// Package bot 实现 Reasonix 多渠道 IM bot 消息网关，支持 QQ、飞书、微信。
// 架构参考 Hermes Agent 的 gateway/adapter/session 模式。
package bot

import (
	"context"
	"strings"
)

// Platform 标识 IM 平台。
type Platform string

const (
	PlatformQQ     Platform = "qq"
	PlatformFeishu Platform = "feishu"
	PlatformWeixin Platform = "weixin"
)

// ChatType 标识会话类型。
type ChatType string

const (
	ChatDM     ChatType = "dm"
	ChatGroup  ChatType = "group"
	ChatGuild  ChatType = "guild"
	ChatDirect ChatType = "direct"
	ChatThread ChatType = "thread"
)

// SessionSource 是会话的复合标识，用于生成稳定的 session key。
type SessionSource struct {
	Platform     Platform `json:"platform"`
	ConnectionID string   `json:"connection_id,omitempty"`
	Domain       string   `json:"domain,omitempty"`
	ChatType     ChatType `json:"chat_type"`
	ChatID       string   `json:"chat_id"`
	UserID       string   `json:"user_id"`
	ThreadID     string   `json:"thread_id,omitempty"`
}

// InboundMedia is an authenticated inbound attachment. Adapters may provide
// Data directly, or a lazy Load callback for platform resources that must not
// be fetched until the gateway has admitted the sender through its allowlist.
type InboundMedia struct {
	Name        string                                        `json:"name,omitempty"`
	MIME        string                                        `json:"mime,omitempty"`
	Data        []byte                                        `json:"-"`
	Load        func(context.Context) ([]byte, string, error) `json:"-"`
	FailureText string                                        `json:"-"`
}

// InboundMessage 是从任一平台收到的入站消息。
type InboundMessage struct {
	Platform     Platform `json:"platform"`
	ConnectionID string   `json:"connection_id,omitempty"`
	Domain       string   `json:"domain,omitempty"`
	ChatType     ChatType `json:"chat_type"`
	ChatID       string   `json:"chat_id"`
	UserID       string   `json:"user_id"`
	UserName     string   `json:"user_name"`
	// OperatorID, when set, is the authenticated actor gated by the allowlist; UserID stays routing-only.
	OperatorID string         `json:"operator_id,omitempty"`
	Text       string         `json:"text"`
	MessageID  string         `json:"message_id"`
	ThreadID   string         `json:"thread_id,omitempty"`
	MediaURLs  []string       `json:"media_urls,omitempty"`
	Media      []InboundMedia `json:"-"`
	// ResolveUserName performs optional platform enrichment after admission.
	// UserName remains the safe fallback when the callback is nil or fails.
	ResolveUserName func(context.Context) string `json:"-"`
	Raw             any                          `json:"-"`
}

// Session derives the SessionSource from this message.
func (m InboundMessage) Session() SessionSource {
	return SessionSource{
		Platform:     m.Platform,
		ConnectionID: m.ConnectionID,
		Domain:       m.Domain,
		ChatType:     m.ChatType,
		ChatID:       m.ChatID,
		UserID:       m.UserID,
		ThreadID:     m.ThreadID,
	}
}

// OutboundMessage 是发送到平台的消息。
type OutboundMessage struct {
	ConnectionID string           `json:"connection_id,omitempty"`
	Domain       string           `json:"domain,omitempty"`
	ChatID       string           `json:"chat_id"`
	ChatType     ChatType         `json:"chat_type,omitempty"`
	Text         string           `json:"text,omitempty"`
	MediaURLs    []string         `json:"media_urls,omitempty"`
	ReplyToMsgID string           `json:"reply_to_msg_id,omitempty"`
	Keyboard     *InlineKeyboard  `json:"keyboard,omitempty"`
	Card         *InteractiveCard `json:"card,omitempty"`
}

// InlineKeyboard 是内联键盘（用于 QQ 审批）。
type InlineKeyboard struct {
	Rows []InlineKeyboardRow `json:"rows"`
}

// InlineKeyboardRow 是一行按钮。
type InlineKeyboardRow struct {
	Buttons []InlineKeyboardButton `json:"buttons"`
}

// InlineKeyboardButton 是一个按钮。
type InlineKeyboardButton struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Style      int    `json:"style,omitempty"` // 0=default, 1=primary, 2=danger
	CallbackID string `json:"callback_id,omitempty"`
}

// InteractiveCard 是交互式卡片（用于飞书审批/问答）。
type InteractiveCard struct {
	Header   string                   `json:"header"`
	Elements []InteractiveCardElement `json:"elements"`
}

// InteractiveCardElement 是卡片内元素。
type InteractiveCardElement struct {
	Tag     string         `json:"tag"`
	Content string         `json:"content,omitempty"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// SendResult 是发送消息的结果。
type SendResult struct {
	MessageID  string   `json:"message_id,omitempty"`
	MessageIDs []string `json:"message_ids,omitempty"`
	Err        error    `json:"err,omitempty"`
}

// DeliveredMessageIDs returns every known delivered message ID, including the
// legacy singular MessageID field, in delivery order without duplicates.
func (r SendResult) DeliveredMessageIDs() []string {
	ids := make([]string, 0, len(r.MessageIDs)+1)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		for _, existing := range ids {
			if existing == id {
				return
			}
		}
		ids = append(ids, id)
	}
	for _, id := range r.MessageIDs {
		add(id)
	}
	add(r.MessageID)
	return ids
}

// Merge appends delivered IDs from another send while keeping MessageID as the
// last delivered ID for callers using the legacy singular field.
func (r *SendResult) Merge(delivered SendResult) {
	for _, id := range delivered.DeliveredMessageIDs() {
		duplicate := false
		for _, existing := range r.MessageIDs {
			if existing == id {
				duplicate = true
				break
			}
		}
		if !duplicate {
			r.MessageIDs = append(r.MessageIDs, id)
		}
		r.MessageID = id
	}
}

// Adapter 是平台适配器接口，每个平台实现一个。
type Adapter interface {
	// Platform 返回平台标识。
	Platform() Platform

	// Start 启动适配器，连接平台 gateway。
	Start(ctx context.Context) error

	// Stop 优雅关闭适配器。
	Stop() error

	// Send 发送一条出站消息。
	Send(ctx context.Context, msg OutboundMessage) (SendResult, error)

	// SendTyping 发送"正在输入"状态。
	SendTyping(ctx context.Context, chatID string) error

	// Messages 返回入站消息通道。
	Messages() <-chan InboundMessage

	// Name 返回适配器实例名（用于日志）。
	Name() string
}

// MessageHandler 是 BotGateway 处理入站消息的回调。
type MessageHandler func(ctx context.Context, msg InboundMessage)
