package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/bot"
)

// New creates a Telegram Bot API adapter.
func New(cfg Config) bot.Adapter {
	return newAdapter(cfg)
}

type adapter struct {
	cfg    Config
	client *client
	msgCh  chan bot.InboundMessage

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	botUser user
	offset  int64
	dedup   *recentUpdates
}

func newAdapter(cfg Config) *adapter {
	return &adapter{
		cfg:    cfg,
		client: newClient(cfg.Token, cfg.APIBaseURL, cfg.HTTPClient),
		msgCh:  make(chan bot.InboundMessage, 64),
		dedup:  newRecentUpdates(1024),
	}
}

func (a *adapter) Platform() bot.Platform              { return bot.PlatformTelegram }
func (a *adapter) Name() string                        { return "telegram" }
func (a *adapter) Messages() <-chan bot.InboundMessage { return a.msgCh }

func (a *adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("telegram adapter already started")
	}
	a.mu.Unlock()

	me, err := a.client.getMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram startup validation failed: %w", err)
	}
	if !me.IsBot {
		return errors.New("telegram startup validation failed: getMe returned a non-bot account")
	}
	if err := a.client.deleteWebhook(ctx); err != nil {
		return fmt.Errorf("telegram startup validation failed: deleteWebhook: %w", err)
	}
	pollCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.botUser = me
	a.cancel = cancel
	a.wg.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.wg.Done()
		defer close(a.msgCh)
		a.poll(pollCtx)
	}()
	return nil
}

func (a *adapter) Stop() error {
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.wg.Wait()
	return nil
}

func (a *adapter) Send(ctx context.Context, outgoing bot.OutboundMessage) (bot.SendResult, error) {
	chunks := splitText(outgoing.Text)
	var result bot.SendResult
	replyTo, err := strconv.ParseInt(outgoing.ReplyToMsgID, 10, 64)
	if outgoing.ReplyToMsgID != "" && err != nil {
		return result, fmt.Errorf("telegram reply message ID: %w", err)
	}
	threadID, err := strconv.ParseInt(outgoing.ThreadID, 10, 64)
	if outgoing.ThreadID != "" && err != nil {
		return result, fmt.Errorf("telegram thread ID: %w", err)
	}
	for i, chunk := range chunks {
		message, err := a.client.sendMessage(ctx, outgoing.ChatID, chunk, replyTo, threadID)
		if err != nil {
			return result, err
		}
		id := strconv.FormatInt(message.MessageID, 10)
		result.MessageIDs = append(result.MessageIDs, id)
		result.MessageID = id
		if i == 0 {
			replyTo = 0
		}
	}
	return result, nil
}

func (a *adapter) SendTyping(ctx context.Context, chatID string) error {
	return a.client.sendChatAction(ctx, chatID)
}

func (a *adapter) poll(ctx context.Context) {
	for ctx.Err() == nil {
		updates, err := a.client.getUpdates(ctx, a.offset)
		if err != nil {
			if ctx.Err() != nil || a.waitAfterError(ctx, err) {
				return
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID < a.offset || a.dedup.has(update.UpdateID) {
				continue
			}
			if inbound, ok := a.inbound(update); ok {
				select {
				case a.msgCh <- inbound:
					a.offset = update.UpdateID + 1
					a.dedup.add(update.UpdateID)
				case <-ctx.Done():
					return
				}
				continue
			}
			a.offset = update.UpdateID + 1
			a.dedup.add(update.UpdateID)
		}
	}
}

// waitAfterError returns true for permanent errors or cancellation.
func (a *adapter) waitAfterError(ctx context.Context, err error) bool {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		if apiErr.code == 401 || apiErr.code == 409 {
			return true
		}
		if apiErr.code == 429 && apiErr.retryAfter > 0 {
			select {
			case <-time.After(apiErr.retryAfter):
				return false
			case <-ctx.Done():
				return true
			}
		}
	}
	select {
	case <-time.After(time.Second):
		return false
	case <-ctx.Done():
		return true
	}
}

func (a *adapter) inbound(update update) (bot.InboundMessage, bool) {
	message := update.Message
	if message == nil || message.From == nil || message.Text == "" {
		return bot.InboundMessage{}, false
	}
	var chatType bot.ChatType
	switch message.Chat.Type {
	case "private":
		chatType = bot.ChatDM
	case "group", "supergroup":
		chatType = bot.ChatGroup
	default:
		return bot.InboundMessage{}, false
	}
	if message.From.ID == a.botUser.ID {
		return bot.InboundMessage{}, false
	}
	text := message.Text
	if chatType == bot.ChatGroup {
		mentionText, mentioned := normalizeGroupText(text, a.botUser.Username)
		commandText, command := normalizeAddressedCommand(text, a.botUser.Username)
		replied := message.ReplyToMessage != nil && message.ReplyToMessage.From != nil && message.ReplyToMessage.From.ID == a.botUser.ID
		if !mentioned && !command && !replied {
			return bot.InboundMessage{}, false
		}
		if mentioned {
			text = mentionText
		} else if command {
			text = commandText
		}
	}
	threadID := ""
	if message.MessageThreadID != 0 {
		threadID = strconv.FormatInt(message.MessageThreadID, 10)
	}
	userName := strings.TrimSpace(message.From.Username)
	if userName == "" {
		userName = strings.TrimSpace(strings.Join([]string{message.From.FirstName, message.From.LastName}, " "))
	}
	return bot.InboundMessage{
		Platform: bot.PlatformTelegram, ConnectionID: a.cfg.ConnectionID, Domain: "telegram", ChatType: chatType,
		ChatID: strconv.FormatInt(message.Chat.ID, 10), UserID: strconv.FormatInt(message.From.ID, 10),
		UserName: userName, Text: text, MessageID: strconv.FormatInt(message.MessageID, 10), ThreadID: threadID,
		Raw: update,
	}, true
}

type recentUpdates struct {
	max   int
	ids   map[int64]struct{}
	order []int64
}

func newRecentUpdates(max int) *recentUpdates {
	return &recentUpdates{max: max, ids: make(map[int64]struct{})}
}
func (d *recentUpdates) has(id int64) bool {
	_, exists := d.ids[id]
	return exists
}

func (d *recentUpdates) add(id int64) {
	if d.has(id) {
		return
	}
	d.ids[id] = struct{}{}
	d.order = append(d.order, id)
	if len(d.order) > d.max {
		delete(d.ids, d.order[0])
		d.order = d.order[1:]
	}
}
