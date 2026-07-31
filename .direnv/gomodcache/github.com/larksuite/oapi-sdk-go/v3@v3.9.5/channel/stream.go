package channel

import (
	"context"
	"fmt"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	defaultThrottleInterval = 500 * time.Millisecond
)

// throttleController handles debouncing and rate limiting of updates.
type throttleController struct {
	mu           sync.Mutex
	interval     time.Duration
	lastExecTime time.Time
	timer        *time.Timer
	isClosed     bool

	// exec is the function that performs the actual update.
	exec func(ctx context.Context) error
}

func newThrottleController(interval time.Duration, exec func(ctx context.Context) error) *throttleController {
	if interval <= 0 {
		interval = defaultThrottleInterval
	}
	return &throttleController{
		interval: interval,
		exec:     exec,
	}
}

// Trigger queues an update, executing it immediately if the interval has passed,
// or scheduling it if an update occurred recently.
func (t *throttleController) Trigger(ctx context.Context) error {
	t.mu.Lock()
	if t.isClosed {
		t.mu.Unlock()
		return fmt.Errorf("stream is closed")
	}

	now := time.Now()
	if now.Sub(t.lastExecTime) >= t.interval {
		// Can execute immediately
		if t.timer != nil {
			t.timer.Stop()
			t.timer = nil
		}
		t.lastExecTime = now
		t.mu.Unlock()

		return t.exec(ctx)
	}

	// Need to throttle
	if t.timer == nil {
		waitDur := t.interval - now.Sub(t.lastExecTime)
		t.timer = time.AfterFunc(waitDur, func() {
			t.mu.Lock()
			if t.isClosed {
				t.mu.Unlock()
				return
			}
			t.lastExecTime = time.Now()
			t.timer = nil
			t.mu.Unlock()

			// We use a background context here since the original context might have been canceled
			// or expired by the time the timer fires. Alternatively, we could capture the ctx,
			// but a background context is safer for delayed execution.
			_ = t.exec(context.Background())
		})
	}
	t.mu.Unlock()
	return nil
}

// Flush executes the update immediately, cancelling any pending timer.
func (t *throttleController) Flush(ctx context.Context) error {
	t.mu.Lock()
	if t.isClosed {
		t.mu.Unlock()
		return fmt.Errorf("stream is closed")
	}

	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.lastExecTime = time.Now()
	t.mu.Unlock()

	return t.exec(ctx)
}

// Close flushes any pending updates and prevents further updates.
func (t *throttleController) Close(ctx context.Context) error {
	t.mu.Lock()
	if t.isClosed {
		t.mu.Unlock()
		return nil
	}

	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
		t.mu.Unlock()
		// If there was a pending timer, execute one last time
		err := t.exec(ctx)

		t.mu.Lock()
		t.isClosed = true
		t.mu.Unlock()
		return err
	}

	t.isClosed = true
	t.mu.Unlock()
	return nil
}

// MarkdownStreamController is a StreamController for markdown text.
type MarkdownStreamController struct {
	client     *lark.Client
	config     types.ChannelConfig
	messageID  string
	title      string
	content    string
	chunkIndex int

	mu       sync.Mutex
	throttle *throttleController
}

// NewMarkdownStreamController creates a new StreamController for updating markdown messages.
func NewMarkdownStreamController(client *lark.Client, config types.ChannelConfig, messageID, initialContent, title string) *MarkdownStreamController {
	m := &MarkdownStreamController{
		client:     client,
		config:     config,
		messageID:  messageID,
		title:      title,
		content:    initialContent,
		chunkIndex: 0,
	}

	m.throttle = newThrottleController(config.Outbound.StreamThrottleMs, m.doUpdate)
	return m
}

// Append appends text to the markdown stream and triggers a throttled update.
func (m *MarkdownStreamController) Append(ctx context.Context, text string) error {
	m.mu.Lock()
	m.content += text
	m.mu.Unlock()

	return m.throttle.Trigger(ctx)
}

// UpdateCard is not supported for MarkdownStreamController.
func (m *MarkdownStreamController) UpdateCard(ctx context.Context, card string) error {
	return fmt.Errorf("UpdateCard is not supported for MarkdownStreamController, use Append")
}

// Flush forces an immediate update of the message.
func (m *MarkdownStreamController) Flush(ctx context.Context) error {
	return m.throttle.Flush(ctx)
}

// Close flushes the stream and closes it.
func (m *MarkdownStreamController) Close(ctx context.Context) error {
	return m.throttle.Close(ctx)
}

func (m *MarkdownStreamController) doUpdate(ctx context.Context) error {
	m.mu.Lock()
	currentContent := m.content
	currentIndex := m.chunkIndex
	currentMessageID := m.messageID
	m.mu.Unlock()

	chunks := outbound.SplitWithCodeFences(currentContent, m.config.Outbound.TextChunkLimit)
	if len(chunks) == 0 {
		return nil
	}

	targetIndex := len(chunks) - 1
	targetChunk := chunks[targetIndex]

	// Convert markdown to rich text (post)
	contentStr, err := normalize.SimpleMarkdownToPost(m.title, targetChunk, nil)
	if err != nil {
		return fmt.Errorf("failed to marshal post content: %w", err)
	}
	msgType := "post"

	if targetIndex > currentIndex {
		// Create a new message for the new chunk
		// We need to reply to the previous chunk to keep the thread?
		// Actually, in stream.go we don't have the original ReceiveID easily accessible.
		// But we can reply to the previous chunk!
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(currentMessageID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType(msgType).
				Content(contentStr).
				Build()).
			Build()

		op := func(attempt int) (interface{}, error) {
			resp, err := m.client.Im.V1.Message.Reply(ctx, req)
			if err != nil {
				return nil, fmt.Errorf("failed to create new message chunk: %w", err)
			}
			if !resp.Success() {
				return nil, &larkcore.CodeError{Code: resp.Code, Msg: resp.Msg}
			}
			return resp, nil
		}
		res, err := outbound.Retry(ctx, op, &outbound.RetryOptions{
			MaxAttempts: m.config.Outbound.Retry.MaxAttempts,
			BaseDelay:   m.config.Outbound.Retry.BaseDelayMs,
		})
		if err != nil {
			return err
		}
		resp := res.(*larkim.ReplyMessageResp)

		m.mu.Lock()
		m.messageID = *resp.Data.MessageId
		m.chunkIndex = targetIndex
		m.mu.Unlock()
		return nil
	}

	// Update existing chunk
	req := larkim.NewUpdateMessageReqBuilder().
		MessageId(currentMessageID).
		Body(larkim.NewUpdateMessageReqBodyBuilder().
			MsgType(msgType).
			Content(contentStr).
			Build()).
		Build()

	op := func(attempt int) (interface{}, error) {
		resp, err := m.client.Im.V1.Message.Update(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to update message: %w", err)
		}
		if !resp.Success() {
			return nil, &larkcore.CodeError{Code: resp.Code, Msg: resp.Msg}
		}
		return resp, nil
	}

	_, err = outbound.Retry(ctx, op, &outbound.RetryOptions{
		MaxAttempts: m.config.Outbound.Retry.MaxAttempts,
		BaseDelay:   m.config.Outbound.Retry.BaseDelayMs,
	})
	return err
}
