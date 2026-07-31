package types

import (
	"context"
)

// Channel is the core interface for the channel feature, providing high-level
// abstractions for message receiving, sending, and streaming.
type Channel interface {
	Send(ctx context.Context, input *SendInput) (*SendResult, error)
	OnMessage(handler func(ctx context.Context, msg *NormalizedMessage) error)
	OnReaction(handler func(ctx context.Context, event *ReactionEvent) error)
	OnComment(handler func(ctx context.Context, event *CommentEvent) error)
	OnBotAdded(handler func(ctx context.Context, event *BotAddedEvent) error)
	OnCardAction(handler func(ctx context.Context, event *CardActionEvent) error)
	OnReject(handler func(ctx context.Context, event *RejectEvent) error)

	// Media download
	DownloadFile(ctx context.Context, fileKey string, mediaType string) ([]byte, error)

	// Lifecycle hooks
	OnReady(handler func())
	OnError(handler func(err error))
	OnReconnecting(handler func())
	OnReconnected(handler func())
	OnDisconnected(handler func())

	// Lifecycle
	Start(ctx context.Context) error
	Stream(ctx context.Context, input *SendInput) (StreamController, error)
	UpdatePolicy(cfg PolicyConfig)
	GetPolicy() PolicyConfig

	// Fetch Bot Identity
	GetBotIdentity(ctx context.Context) *BotIdentity

	// Lifecycle
	Stop(ctx context.Context) error
}

// StreamController provides methods to control a streaming message.
type StreamController interface {
	// Append adds text to the end of the streaming message.
	Append(ctx context.Context, text string) error
	// UpdateCard updates the message with a new card.
	UpdateCard(ctx context.Context, card string) error
	// Flush forces any pending text to be sent immediately.
	Flush(ctx context.Context) error
	// Close completes the stream. No further updates can be made.
	Close(ctx context.Context) error
}

// NormalizedMessage represents a standardized message event extracted from various
// underlying message types, making it easier to handle common use cases.
type NormalizedMessage struct {
	EventID        string      `json:"event_id"` // Original event ID for tracing/debugging
	MessageID      string      `json:"message_id"`
	ChatID         string      `json:"chat_id"`
	ChatType       string      `json:"chat_type"` // "group" or "p2p"
	UserID         string      `json:"user_id"`
	Content        string      `json:"content"`          // Standard text content
	RawContentType string      `json:"raw_content_type"` // Original message type from Lark API
	Mentions       []Mention   `json:"mentions"`         // Mentions in the message
	MentionAll     bool        `json:"mention_all"`
	MentionedBot   bool        `json:"mentioned_bot"`
	Resources      []Resource  `json:"resources"` // Images, files, etc.
	CreateTimeMs   int64       `json:"create_time_ms"`
	RawEvent       interface{} `json:"raw_event"` // Original event data
}

// Mention represents a user mention in a message.
type Mention struct {
	Key    string `json:"key"`
	UserID string `json:"user_id"` // UserID if available
	OpenID string `json:"open_id"` // OpenID if available
	Name   string `json:"name"`
	IsBot  bool   `json:"is_bot"`
}

// Resource represents an attached file, image, audio, video, sticker, etc.
type Resource struct {
	Type          string `json:"type"`     // "image", "file", "audio", "video", "sticker", etc.
	FileKey       string `json:"file_key"` // The file key used in Lark API
	FileName      string `json:"file_name,omitempty"`
	DurationMs    *int   `json:"duration_ms,omitempty"`
	CoverImageKey string `json:"cover_image_key,omitempty"`
}

// SendInput represents the structured parameters for sending a message.
type SendInput struct {
	ReceiveID      string `json:"receive_id,omitempty"` // High priority, auto-detected
	ChatID         string `json:"chat_id,omitempty"`    // Fallback
	UserID         string `json:"user_id,omitempty"`    // Fallback
	MsgType        string `json:"msg_type,omitempty"`   // text, post, image, file, interactive, etc.
	ReplyMessageID string `json:"reply_message_id,omitempty"`

	// Message contents
	Text           string `json:"text,omitempty"`
	Markdown       string `json:"markdown,omitempty"`
	Title          string `json:"title,omitempty"` // Used as Post title if Markdown is set
	ImageKey       string `json:"image_key,omitempty"`
	FileKey        string `json:"file_key,omitempty"`
	AudioKey       string `json:"audio_key,omitempty"`
	VideoKey       string `json:"video_key,omitempty"`
	Card           string `json:"card,omitempty"` // Stringified JSON of the card
	Post           string `json:"post,omitempty"` // Stringified JSON of the post
	ShareChatID    string `json:"share_chat_id,omitempty"`
	ShareUserID    string `json:"share_user_id,omitempty"`
	StickerFileKey string `json:"sticker_file_key,omitempty"`

	// Local file paths (Uploader will automatically upload them before sending)
	ImagePath string `json:"image_path,omitempty"`
	FilePath  string `json:"file_path,omitempty"`

	// Media configuration for uploading
	Media *UploadInput `json:"media,omitempty"`

	// Mentions represents user mentions to be prepended to the message
	Mentions []Mention `json:"mentions,omitempty"`
}

// SendResult represents the result of sending a message.
type SendResult struct {
	MessageID string   `json:"message_id"`
	ChunkIDs  []string `json:"chunk_ids,omitempty"` // For messages split into multiple chunks
	ChatID    string   `json:"chat_id,omitempty"`
	Error     error    `json:"error,omitempty"`
}

type MediaKind string

const (
	MediaKindImage MediaKind = "image"
	MediaKindFile  MediaKind = "file"
	MediaKindAudio MediaKind = "audio"
	MediaKindVideo MediaKind = "video"
)

type UploadInput struct {
	Kind        MediaKind
	SourceURL   string
	SourcePath  string
	SourceBytes []byte
	FileName    string
	Duration    *int
}

type UploadResult struct {
	Kind       MediaKind
	FileKey    string
	DurationMs *int
}
