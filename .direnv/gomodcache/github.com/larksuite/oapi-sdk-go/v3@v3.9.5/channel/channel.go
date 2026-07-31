package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
	"github.com/larksuite/oapi-sdk-go/v3/channel/pipeline"
	"github.com/larksuite/oapi-sdk-go/v3/channel/safety"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func commentDedupKey(event *types.CommentEvent) string {
	return fmt.Sprintf("comment:%s:%s", event.FileToken, event.CommentID)
}

func reactionDedupKey(event *types.ReactionEvent) string {
	return fmt.Sprintf(
		"rx:%s:%s:%s:%s:%d",
		event.MessageID,
		event.UserID,
		event.ReactionType,
		event.Action,
		event.CreateTimeMs,
	)
}

func cardActionDedupKey(event *types.CardActionEvent) string {
	if event.EventID != "" {
		return event.EventID
	}
	return fmt.Sprintf(
		"card:%s:%s:%s",
		event.MessageID,
		cardActionActorID(event.Operator),
		cardActionID(event.Action),
	)
}

func cardActionActorID(operator types.CardActionOperator) string {
	if operator.OpenID != "" {
		return operator.OpenID
	}
	return operator.UserID
}

func cardActionID(action types.CardActionPayload) string {
	payload := struct {
		Tag        string                 `json:"tag,omitempty"`
		Name       string                 `json:"name,omitempty"`
		Option     string                 `json:"option,omitempty"`
		Value      map[string]interface{} `json:"value,omitempty"`
		FormValue  map[string]interface{} `json:"form_value,omitempty"`
		InputValue string                 `json:"input_value,omitempty"`
		Options    []string               `json:"options,omitempty"`
		Checked    bool                   `json:"checked,omitempty"`
	}{
		Tag:        action.Tag,
		Name:       action.Name,
		Option:     action.Option,
		Value:      action.Value,
		FormValue:  action.FormValue,
		InputValue: action.InputValue,
		Options:    action.Options,
		Checked:    action.Checked,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return action.Tag
	}
	return string(b)
}

// channelImpl is the default implementation of the Channel interface.
type channelImpl struct {
	client          *lark.Client
	wsClient        *larkws.Client
	config          types.ChannelConfig
	uploader        outbound.Uploader
	dedupCache      *safety.DedupCache
	pipelineManager *pipeline.ChatPipelineManager
	policyGate      *safety.PolicyGate
	processLock     *safety.ProcessingLock
	staleWindow     time.Duration

	botIdentity              *types.BotIdentity
	botIdentityFetchedAt     time.Time
	botIdentityLastFailureAt time.Time
	botMu                    sync.Mutex

	// Handler registries
	onMessageHandlers    []func(ctx context.Context, msg *types.NormalizedMessage) error
	onCommentHandlers    []func(ctx context.Context, event *types.CommentEvent) error
	onReactionHandlers   []func(ctx context.Context, event *types.ReactionEvent) error
	onBotAddedHandlers   []func(ctx context.Context, event *types.BotAddedEvent) error
	onCardActionHandlers []func(ctx context.Context, event *types.CardActionEvent) error
	onRejectHandlers     []func(ctx context.Context, event *types.RejectEvent) error

	onReadyHandlers        []func()
	onErrorHandlers        []func(err error)
	onReconnectingHandlers []func()
	onReconnectedHandlers  []func()
	onDisconnectedHandlers []func()

	messageHandlerReg  bool
	commentHandlerReg  bool
	reactionHandlerReg bool
	botAddedHandlerReg bool
}

// NewChannel creates a new Channel instance with the provided Lark API client and WebSocket client.
func NewChannel(client *lark.Client, wsClient *larkws.Client, opts ...types.ChannelOption) types.Channel {
	cfg := types.DefaultChannelConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.BotIdentityCache.TTL <= 0 {
		cfg.BotIdentityCache.TTL = 30 * time.Minute
	}
	if cfg.BotIdentityCache.MinRefreshInterval <= 0 {
		cfg.BotIdentityCache.MinRefreshInterval = 1 * time.Minute
	} else if cfg.BotIdentityCache.MinRefreshInterval < 30*time.Second {
		cfg.BotIdentityCache.MinRefreshInterval = 30 * time.Second
	}

	return &channelImpl{
		client:          client,
		wsClient:        wsClient,
		config:          cfg,
		uploader:        outbound.NewUploader(client),
		dedupCache:      safety.NewDedupCache(cfg.Safety.Dedup.MaxEntries, cfg.Safety.Dedup.SweepIntervalMs),
		pipelineManager: pipeline.NewChatPipelineManager(cfg.Safety.Batch),
		policyGate:      safety.NewPolicyGate(&cfg.Policy, nil),
		processLock:     safety.NewProcessingLock(types.DefaultLockTTL, 1*time.Minute),
		staleWindow:     cfg.Safety.StaleMessageWindowMs,
	}
}

// GetBotIdentity fetches and caches the bot's identity from the server.
func (ch *channelImpl) GetBotIdentity(ctx context.Context) *types.BotIdentity {
	if ch.cachedBotIdentityFresh() {
		return ch.botIdentity
	}

	ch.botMu.Lock()
	defer ch.botMu.Unlock()
	if ch.cachedBotIdentityFresh() {
		return ch.botIdentity
	}

	now := time.Now()
	if ch.shouldThrottleBotIdentityRefresh(now) {
		return ch.botIdentity
	}

	identity, err := ch.fetchBotIdentity(ctx)
	if err != nil {
		ch.botIdentityLastFailureAt = now
		if ch.botIdentity != nil {
			larkcore.NewEventLogger().Warn(ctx, fmt.Sprintf("[Channel] Failed to refresh bot info, using stale cache. Err: %v", err))
			return ch.botIdentity
		}
		larkcore.NewEventLogger().Error(ctx, fmt.Sprintf("[Channel] Failed to fetch bot info. Err: %v", err))
		return nil
	}

	ch.botIdentity = identity
	ch.botIdentityFetchedAt = time.Now()
	ch.botIdentityLastFailureAt = time.Time{}
	return ch.botIdentity
}

func (ch *channelImpl) cachedBotIdentityFresh() bool {
	if ch.botIdentity == nil {
		return false
	}
	ttl := ch.config.BotIdentityCache.TTL
	if ttl <= 0 {
		return true
	}
	return time.Since(ch.botIdentityFetchedAt) < ttl
}

func (ch *channelImpl) shouldThrottleBotIdentityRefresh(now time.Time) bool {
	if ch.botIdentityLastFailureAt.IsZero() {
		return false
	}
	minInterval := ch.config.BotIdentityCache.MinRefreshInterval
	if minInterval <= 0 {
		return false
	}
	return now.Sub(ch.botIdentityLastFailureAt) < minInterval
}

func (ch *channelImpl) fetchBotIdentity(ctx context.Context) (*types.BotIdentity, error) {
	// Fetch bot info using the official SDK raw method since bot/v3/info is not generated
	resp, err := ch.client.Get(ctx, "/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty response from bot/v3/info")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code from bot/v3/info: %d", resp.StatusCode)
	}
	larkcore.NewEventLogger().Debug(ctx, fmt.Sprintf("[Channel] bot/v3/info response received. status=%d raw_body_bytes=%d", resp.StatusCode, len(resp.RawBody)))

	var result struct {
		Code int `json:"code"`
		Bot  struct {
			OpenId         string `json:"open_id"`
			AppName        string `json:"app_name"`
			ActivateStatus int    `json:"activate_status"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(resp.RawBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse bot info: %w", err)
	}
	larkcore.NewEventLogger().Debug(ctx, fmt.Sprintf("[Channel] Parsed bot/v3/info payload. code=%d open_id=%q app_name=%q activate_status=%d", result.Code, result.Bot.OpenId, result.Bot.AppName, result.Bot.ActivateStatus))
	if result.Code != 0 {
		return nil, fmt.Errorf("bot info returned non-zero code: %d", result.Code)
	}

	botName := result.Bot.AppName
	if botName == "" {
		larkcore.NewEventLogger().Debug(ctx, fmt.Sprintf("[Channel] bot/v3/info app_name is empty, using default name. open_id=%q activate_status=%d", result.Bot.OpenId, result.Bot.ActivateStatus))
		botName = "bot"
	}

	identity := &types.BotIdentity{
		OpenID:         result.Bot.OpenId,
		Name:           botName,
		ActivateStatus: result.Bot.ActivateStatus,
	}
	larkcore.NewEventLogger().Debug(ctx, fmt.Sprintf("[Channel] Resolved bot identity. open_id=%q name=%q activate_status=%d", identity.OpenID, identity.Name, identity.ActivateStatus))
	return identity, nil
}

// OnReady registers a handler for WS ready events.
func (ch *channelImpl) OnReady(handler func()) {
	ch.onReadyHandlers = append(ch.onReadyHandlers, handler)
}

// OnError registers a handler for WS error events.
func (ch *channelImpl) OnError(handler func(err error)) {
	ch.onErrorHandlers = append(ch.onErrorHandlers, handler)
}

// OnReconnecting registers a handler for WS reconnecting events.
func (ch *channelImpl) OnReconnecting(handler func()) {
	ch.onReconnectingHandlers = append(ch.onReconnectingHandlers, handler)
}

// OnReconnected registers a handler for WS reconnected events.
func (ch *channelImpl) OnReconnected(handler func()) {
	ch.onReconnectedHandlers = append(ch.onReconnectedHandlers, handler)
}

// OnDisconnected registers a handler for WS disconnected events.
func (ch *channelImpl) OnDisconnected(handler func()) {
	ch.onDisconnectedHandlers = append(ch.onDisconnectedHandlers, handler)
}

// UpdatePolicy updates the policy configuration for the channel.
func (ch *channelImpl) UpdatePolicy(cfg types.PolicyConfig) {
	ch.policyGate.UpdateConfig(cfg)
}

// GetPolicy returns the current policy configuration.
func (ch *channelImpl) GetPolicy() types.PolicyConfig {
	return ch.policyGate.GetConfig()
}

// Start starts the underlying WebSocket client and wires up lifecycle events.
func (ch *channelImpl) Start(ctx context.Context) error {
	if ch.wsClient == nil {
		larkcore.NewEventLogger().Info(ctx, "[Channel] Start called but wsClient is nil, skipping WebSocket connection.")
		return nil
	}
	ch.wsClient.SetOnReady(func() {
		botInfo := ch.GetBotIdentity(ctx)
		botIdentityStr := ""
		if botInfo != nil {
			botIdentityStr = fmt.Sprintf("botIdentity: {\n  openId: '%s',\n  name: '%s'\n}", botInfo.OpenID, botInfo.Name)
		}

		larkcore.NewEventLogger().Info(ctx, fmt.Sprintf("receive events or callbacks through persistent connection only available in self-build & Feishu app, Configured in:\n"+
			"    Developer Console(开发者后台) \n"+
			"        ->\n"+
			"    Events and Callbacks(事件与回调)\n"+
			"        -> \n"+
			"    Mode of event/callback subscription(订阅方式)\n"+
			"        -> \n"+
			"    Receive events/callbacks through persistent connection(使用长连接接收事件/回调)\n\n"+
			"WebSocket 连接成功, %s", botIdentityStr))

		for _, h := range ch.onReadyHandlers {
			h()
		}
	})
	ch.wsClient.SetOnError(func(err error) {
		for _, h := range ch.onErrorHandlers {
			h(err)
		}
	})
	ch.wsClient.SetOnReconnecting(func() {
		for _, h := range ch.onReconnectingHandlers {
			h()
		}
	})
	ch.wsClient.SetOnReconnected(func() {
		for _, h := range ch.onReconnectedHandlers {
			h()
		}
	})
	ch.wsClient.SetOnDisconnected(func() {
		for _, h := range ch.onDisconnectedHandlers {
			h()
		}
	})
	return ch.wsClient.Start(ctx)
}

// Stop gracefully stops the underlying WebSocket client.
func (ch *channelImpl) Stop(ctx context.Context) error {
	if ch.wsClient != nil {
		ch.wsClient.Close()
	}
	return nil
}

// OnMessage registers a handler for NormalizedMessage events.
func (ch *channelImpl) OnMessage(handler func(ctx context.Context, msg *types.NormalizedMessage) error) {
	ch.onMessageHandlers = append(ch.onMessageHandlers, handler)
	ch.ensureMessageHandler()
}

// OnComment registers a handler for CommentEvent.
func (ch *channelImpl) OnComment(handler func(ctx context.Context, event *types.CommentEvent) error) {
	ch.onCommentHandlers = append(ch.onCommentHandlers, handler)

	if ch.commentHandlerReg || ch.wsClient == nil {
		return
	}
	ch.commentHandlerReg = true
	dispatcher := ch.wsClient.EventHandler()
	if dispatcher != nil {
		dispatcher.OnCustomizedEvent("drive.notice.comment_add_v1", func(ctx context.Context, event *larkevent.EventReq) error {
			if len(ch.onCommentHandlers) == 0 {
				return nil
			}
			commentEvent := normalize.ParseComment(event)
			if commentEvent != nil && commentEvent.CommentID != "" {
				dedupKey := commentDedupKey(commentEvent)
				if ch.dedupCache != nil && ch.dedupCache.IsDuplicate(dedupKey) {
					return nil
				}
				if ch.processLock.Acquire(dedupKey) {
					defer ch.processLock.Release(dedupKey)

					// Serialize per document file token
					err := ch.pipelineManager.Run(ctx, commentEvent.FileToken, func() error {
						for _, h := range ch.onCommentHandlers {
							if err := h(ctx, commentEvent); err != nil {
								return err
							}
						}
						return nil
					})
					if err != nil {
						// handle error if needed
					}
				}
			}
			return nil
		})
	}
}

func (ch *channelImpl) ensureMessageHandler() {
	if ch.messageHandlerReg || ch.wsClient == nil {
		return
	}
	ch.messageHandlerReg = true
	dispatcher := ch.wsClient.EventHandler()
	if dispatcher != nil {
		dispatcher.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			// Handle Message
			if len(ch.onMessageHandlers) > 0 {
				normMsg := normalize.ParseMessage(event)
				if normMsg != nil {
					botInfo := ch.GetBotIdentity(ctx)
					if botInfo != nil {
						// 1. Self-reply loop prevention
						if normMsg.UserID == botInfo.OpenID {
							return nil
						}
						// 2. MentionedBot check
						for i := range normMsg.Mentions {
							m := &normMsg.Mentions[i]
							if m.OpenID == botInfo.OpenID || m.UserID == botInfo.OpenID || (botInfo.UserID != "" && m.UserID == botInfo.UserID) {
								normMsg.MentionedBot = true
								m.IsBot = true
							}
						}
					}

					if safety.IsStale(normMsg.CreateTimeMs, ch.staleWindow) {
						// do nothing
					} else if ch.dedupCache != nil && ch.dedupCache.IsDuplicate(normMsg.MessageID) {
						// do nothing
					} else {
						decision := ch.policyGate.Evaluate(normMsg)
						if decision.Allowed {
							if ch.processLock.Acquire(normMsg.MessageID) {
								dispatchHandler := func(ctx context.Context, batch *types.BatchedDispatch) error {
									defer func() {
										for _, id := range batch.SourceIDs {
											ch.processLock.Release(id)
										}
									}()
									for _, h := range ch.onMessageHandlers {
										h(ctx, batch.Message)
									}
									return nil
								}
								ch.pipelineManager.Push(ctx, normMsg.ChatID, normMsg, dispatchHandler)
							}
						} else {
							// Dispatched reject event
							if len(ch.onRejectHandlers) > 0 {
								rejectEvent := &types.RejectEvent{
									MessageID: normMsg.MessageID,
									ChatID:    normMsg.ChatID,
									SenderID:  normMsg.UserID,
									Reason:    string(decision.Reason),
								}
								for _, h := range ch.onRejectHandlers {
									h(ctx, rejectEvent)
								}
							}
						}
					}
				}
			}

			return nil
		})
	}
}

// OnBotAdded registers a handler for BotAddedEvent.
func (ch *channelImpl) OnBotAdded(handler func(ctx context.Context, event *types.BotAddedEvent) error) {
	ch.onBotAddedHandlers = append(ch.onBotAddedHandlers, handler)
	if ch.botAddedHandlerReg || ch.wsClient == nil {
		return
	}
	ch.botAddedHandlerReg = true
	dispatcher := ch.wsClient.EventHandler()
	if dispatcher != nil {
		dispatcher.OnP2ChatMemberBotAddedV1(func(ctx context.Context, event *larkim.P2ChatMemberBotAddedV1) error {
			if len(ch.onBotAddedHandlers) > 0 {
				botAddedEvent := normalize.ParseBotAdded(event)
				if botAddedEvent != nil {
					if ch.dedupCache != nil && ch.dedupCache.IsDuplicate(botAddedEvent.EventID) {
						return nil
					}
					if !ch.processLock.Acquire(botAddedEvent.EventID) {
						return nil
					}
					defer ch.processLock.Release(botAddedEvent.EventID)

					// Serialize per chat
					err := ch.pipelineManager.Run(ctx, botAddedEvent.ChatID, func() error {
						for _, h := range ch.onBotAddedHandlers {
							if err := h(ctx, botAddedEvent); err != nil {
								return err
							}
						}
						return nil
					})
					if err != nil {
						return err
					}
				}
			}
			return nil
		})
	}
}

// OnReaction registers a handler for ReactionEvent.
func (ch *channelImpl) OnReaction(handler func(ctx context.Context, event *types.ReactionEvent) error) {
	ch.onReactionHandlers = append(ch.onReactionHandlers, handler)
	if ch.reactionHandlerReg || ch.wsClient == nil {
		return
	}
	ch.reactionHandlerReg = true
	dispatcher := ch.wsClient.EventHandler()
	if dispatcher != nil {
		handleReaction := func(ctx context.Context, reactionEvent *types.ReactionEvent) error {
			if reactionEvent != nil {
				dedupKey := reactionDedupKey(reactionEvent)
				if ch.dedupCache != nil && ch.dedupCache.IsDuplicate(dedupKey) {
					return nil
				}
				if !ch.processLock.Acquire(dedupKey) {
					return nil
				}
				defer ch.processLock.Release(dedupKey)

				// Serialize per message
				err := ch.pipelineManager.Run(ctx, reactionEvent.MessageID, func() error {
					for _, h := range ch.onReactionHandlers {
						if err := h(ctx, reactionEvent); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					return err
				}
			}
			return nil
		}

		dispatcher.OnP2MessageReactionCreatedV1(func(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) error {
			if len(ch.onReactionHandlers) > 0 {
				return handleReaction(ctx, normalize.ParseReaction(event))
			}
			return nil
		})
		dispatcher.OnP2MessageReactionDeletedV1(func(ctx context.Context, event *larkim.P2MessageReactionDeletedV1) error {
			if len(ch.onReactionHandlers) > 0 {
				return handleReaction(ctx, normalize.ParseReaction(event))
			}
			return nil
		})
	}
}

// OnCardAction registers a handler for CardActionEvent events.
func (ch *channelImpl) OnCardAction(handler func(ctx context.Context, event *types.CardActionEvent) error) {
	ch.onCardActionHandlers = append(ch.onCardActionHandlers, handler)
	if ch.wsClient != nil {
		dispatcher := ch.wsClient.EventHandler()
		if dispatcher != nil {
			dispatcher.OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
				cardActionEvent := normalize.ParseCardAction(event)
				if cardActionEvent != nil {
					dedupKey := cardActionDedupKey(cardActionEvent)
					// Card actions don't use batching but we can use queueing and locks.
					if ch.dedupCache != nil && ch.dedupCache.IsDuplicate(dedupKey) {
						return nil, nil
					}

					if !ch.processLock.Acquire(dedupKey) {
						return nil, nil
					}
					defer ch.processLock.Release(dedupKey)

					// Queue to serialize per chat
					scope := cardActionEvent.ChatID
					if scope == "" {
						scope = cardActionEvent.MessageID
					}
					err := ch.pipelineManager.Run(ctx, scope, func() error {
						for _, h := range ch.onCardActionHandlers {
							if err := h(ctx, cardActionEvent); err != nil {
								return err
							}
						}
						return nil
					})

					if err != nil {
						return nil, err
					}
				}
				return nil, nil
			})
		}
	}
}

// OnReject registers a handler for messages rejected by safety policies.
func (ch *channelImpl) OnReject(handler func(ctx context.Context, event *types.RejectEvent) error) {
	ch.onRejectHandlers = append(ch.onRejectHandlers, handler)
}

// DownloadFile downloads media by key and type (e.g., "image", "file").
func (ch *channelImpl) DownloadFile(ctx context.Context, fileKey string, mediaType string) ([]byte, error) {
	if fileKey == "" {
		return nil, fmt.Errorf("fileKey cannot be empty")
	}

	if mediaType == "image" {
		req := larkim.NewGetImageReqBuilder().
			ImageKey(fileKey).
			Build()
		resp, err := ch.client.Im.V1.Image.Get(ctx, req)
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, fmt.Errorf("download image API error: %d - %s", resp.Code, resp.Msg)
		}
		// Write the stream to byte array
		var buf bytes.Buffer
		_, err = buf.ReadFrom(resp.File)
		if err != nil {
			return nil, fmt.Errorf("failed to read image stream: %w", err)
		}
		return buf.Bytes(), nil

	} else if mediaType == "file" || mediaType == "audio" || mediaType == "video" || mediaType == "media" {
		req := larkim.NewGetFileReqBuilder().
			FileKey(fileKey).
			Build()
		resp, err := ch.client.Im.V1.File.Get(ctx, req)
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, fmt.Errorf("download file API error: %d - %s", resp.Code, resp.Msg)
		}
		var buf bytes.Buffer
		_, err = buf.ReadFrom(resp.File)
		if err != nil {
			return nil, fmt.Errorf("failed to read file stream: %w", err)
		}
		return buf.Bytes(), nil
	}

	return nil, fmt.Errorf("unsupported mediaType: %s", mediaType)
}

// Stream initiates a streaming message session. It returns a StreamController to append and flush content.
func (ch *channelImpl) Stream(ctx context.Context, input *types.SendInput) (types.StreamController, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}

	if input.Card != "" {
		res, err := ch.Send(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to send initial card message: %w", err)
		}
		return NewCardStreamController(ch.client, ch.config, res.MessageID), nil
	}

	// Ensure we send an initial message to get a MessageID
	// For streaming, we typically want to start with empty or initial markdown text
	if input.Markdown == "" && input.Text == "" {
		input.Markdown = "..."
	}

	res, err := ch.Send(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to send initial message for streaming: %w", err)
	}

	return NewMarkdownStreamController(ch.client, ch.config, res.MessageID, input.Markdown, input.Title), nil
}
