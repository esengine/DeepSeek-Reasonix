package bot

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/sessioninbox"
)

func (gw *BotGateway) dispatchQueueResult(ctx context.Context, adapter Adapter, key string, msg InboundMessage, cleanup func(), result QueueResult) {
	if result.Queued {
		// Unexpected with Cap=max and Drop=new while idle; persist as fallback.
		if rec, err := gw.followupActiveSessionDurable(ctx, adapter, key, msg); err == nil {
			gw.storeReactionCleanup(key, cleanup)
			_ = gw.sendText(ctx, adapter, msg, formatQueuedReceipt(rec))
		} else {
			gw.storeReactionCleanup(key, cleanup)
		}
		return
	}
	if !result.Acquired {
		gw.logger.Debug("session busy without queue action", "session", key[:8])
		gw.storeReactionCleanup(key, cleanup)
		return
	}
	// Keep the dispatch loop free to deliver approval/answer replies while the
	// active turn blocks. The per-session lock still serializes all turns.
	gw.turnWG.Go(func() { gw.runInitialInboxTurn(ctx, adapter, key, msg, cleanup) })
}

func (gw *BotGateway) runInitialInboxTurn(ctx context.Context, adapter Adapter, key string, msg InboundMessage, cleanup func()) {
	profile := gw.sessionProfileForMessage(msg)
	state := gw.getOrCreateSession(ctx, key, msg)
	if state == nil || state.ctrl == nil {
		message := "内部错误：无法创建会话。"
		if profile.sessionPath != "" {
			message = "当前绑定会话正在另一个窗口中使用；本次消息未改绑、未创建重复会话。请稍后重试。"
		}
		_ = gw.sendText(ctx, adapter, msg, message)
		gw.retryProviderIngress(ctx, adapter, msg)
		gw.sessions.Release(key)
		if cleanup != nil {
			cleanup()
		}
		return
	}
	gw.rememberSessionReady(msg, state.ctrl)
	prepared := gw.prepareDurableInboxMessage(ctx, adapter, msg, state)
	api, ok := state.ctrl.(control.SessionAPI)
	if !ok {
		// ControllerFactory historically accepted the smaller Controller
		// contract. Keep custom/test controllers working while production
		// controllers use the durable inbox owner path below.
		gw.logger.Debug("bot controller does not expose durable inbox; using direct turn", "session", key[:8])
		gw.completeIngressMessage(ctx, adapter, msg)
		if gw.cfg.OnRemoteTurnAccepted != nil {
			gw.cfg.OnRemoteTurnAccepted(msg, state.ctrl.SessionPath(), prepared.Text)
		}
		gw.runTurnItem(ctx, adapter, key, prepared, "", cleanup)
		return
	}
	receipt, err := enqueueViaInbox(api, prepared, sessioninbox.IntentFollowup)
	if err != nil {
		gw.logger.Warn("persist initial bot turn failed", "session", key[:8], "err", err)
		_ = gw.sendText(ctx, adapter, msg, "消息暂时无法持久化，请稍后重试。")
		gw.retryProviderIngress(ctx, adapter, msg)
		gw.finishTurnItem(ctx, adapter, key, msg, cleanup)
		return
	}
	gw.completeIngressMessage(ctx, adapter, msg)
	if receipt.Idempotent {
		meta, _, readErr := api.ReadInboxItem(receipt.ItemID)
		if readErr != nil || meta.State != sessioninbox.StateQueued {
			gw.logger.Info("duplicate durable bot turn suppressed", "session", key[:8], "item", receipt.ItemID, "state", meta.State)
			gw.finishTurnItem(ctx, adapter, key, msg, cleanup)
			return
		}
	}
	if gw.cfg.OnRemoteTurnAccepted != nil {
		gw.cfg.OnRemoteTurnAccepted(msg, state.ctrl.SessionPath(), prepared.Text)
	}
	gw.runTurnItem(ctx, adapter, key, prepared, receipt.ItemID, cleanup)
}

func (gw *BotGateway) finishTurnItem(ctx context.Context, adapter Adapter, key string, fallback InboundMessage, cleanup func()) {
	// Legacy in-memory pending first (compat), then durable session inbox.
	next := gw.sessions.Release(key)
	nextInboxID := ""
	if next == nil {
		if queued := gw.nextInboxTurn(key, fallback); queued != nil {
			next = &queued.msg
			nextInboxID = queued.itemID
			// Release made the session idle. If another inbound message wins the
			// lock, leave this disk-backed item queued for that turn's drain.
			if !gw.sessions.TryAcquireIdle(key) {
				if cleanup != nil {
					cleanup()
				}
				return
			}
		}
	}
	if next == nil {
		gw.flushReactionCleanups(key, cleanup)
		return
	}
	if cleanup != nil {
		cleanup()
	}
	nextCleanup := makeReactionCleanup(gw.takeReactionCleanups(key))
	gw.logger.Info("bot pending message released", "platform", next.Platform, "chat_type", next.ChatType, "chat", hashID(next.ChatID), "session", key[:8])
	gw.runTurnItem(ctx, adapter, key, *next, nextInboxID, nextCleanup)
}

func (gw *BotGateway) followupActiveSessionDurable(ctx context.Context, adapter Adapter, key string, msg InboundMessage) (sessioninbox.InboxReceipt, error) {
	api := gw.sessionAPI(key)
	if api == nil {
		return sessioninbox.InboxReceipt{}, fmt.Errorf("no session controller")
	}
	gw.mu.Lock()
	state := gw.controllers[key]
	gw.mu.Unlock()
	msg = gw.prepareDurableInboxMessage(ctx, adapter, msg, state)
	return enqueueViaInbox(api, msg, sessioninbox.IntentFollowup)
}

func (gw *BotGateway) collectActiveSessionDurable(ctx context.Context, adapter Adapter, key string, msg InboundMessage) (sessioninbox.InboxReceipt, error) {
	api := gw.sessionAPI(key)
	if api == nil {
		return sessioninbox.InboxReceipt{}, fmt.Errorf("no session controller")
	}
	gw.mu.Lock()
	state := gw.controllers[key]
	gw.mu.Unlock()
	msg = gw.prepareDurableInboxMessage(ctx, adapter, msg, state)
	return collectAppend(api, msg, gw.sessions.Debounce())
}

func (gw *BotGateway) interruptActiveSessionDurable(ctx context.Context, adapter Adapter, key string, msg InboundMessage) (sessioninbox.InboxReceipt, error) {
	api := gw.sessionAPI(key)
	if api == nil {
		return sessioninbox.InboxReceipt{}, fmt.Errorf("no session controller")
	}
	gw.mu.Lock()
	state := gw.controllers[key]
	gw.mu.Unlock()
	msg = gw.prepareDurableInboxMessage(ctx, adapter, msg, state)
	return interruptEnqueue(api, msg)
}

func (gw *BotGateway) prepareDurableInboxMessage(ctx context.Context, adapter Adapter, msg InboundMessage, state *sessionState) InboundMessage {
	msg.Text = gw.inputTextWithMedia(ctx, adapter, msg, state)
	if msg.ChatType == ChatGroup {
		userName := strings.TrimSpace(msg.UserName)
		if msg.ResolveUserName != nil {
			if resolved := strings.TrimSpace(msg.ResolveUserName(ctx)); resolved != "" {
				userName = resolved
			}
		}
		msg.Text = fmt.Sprintf("[%s] %s", userName, msg.Text)
		msg.UserName = userName
	}
	msg.Media = nil
	msg.MediaURLs = nil
	msg.ResolveUserName = nil
	msg.Raw = nil
	return msg
}

type botInboxTurn struct {
	itemID string
	msg    InboundMessage
}

// nextInboxTurn loads the next durable FIFO follow-up with its original routing
// metadata. RunInboxTurn performs the atomic queued -> running claim.
func (gw *BotGateway) nextInboxTurn(key string, fallback InboundMessage) *botInboxTurn {
	api := gw.sessionAPI(key)
	if api == nil {
		return nil
	}
	snap := api.InboxSnapshot()
	if snap.Paused {
		return nil
	}
	for _, it := range snap.Items {
		if it.State != sessioninbox.StateQueued {
			continue
		}
		_, env, err := api.ReadInboxItem(it.ID)
		if err != nil {
			continue
		}
		msg := botMessageFromEnvelope(env, fallback)
		if _, hasStoredRoute := env.Extra[botInboxMessageExtraKey]; !hasStoredRoute && it.Idempotency != "" {
			msg.MessageID = it.Idempotency
		}
		return &botInboxTurn{itemID: it.ID, msg: msg}
	}
	return nil
}

// nextInboxMessage is retained for focused queue inspection tests.
func (gw *BotGateway) nextInboxMessage(key string) *InboundMessage {
	next := gw.nextInboxTurn(key, InboundMessage{ChatID: key})
	if next == nil {
		return nil
	}
	return &next.msg
}
