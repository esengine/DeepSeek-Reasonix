package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/sessioninbox"
)

const botInboxMessageExtraKey = "reasonix.bot.inbound.v1"

type durableBotMessage struct {
	Platform     Platform `json:"platform"`
	ConnectionID string   `json:"connectionId,omitempty"`
	Domain       string   `json:"domain,omitempty"`
	ChatType     ChatType `json:"chatType"`
	ChatID       string   `json:"chatId"`
	UserID       string   `json:"userId,omitempty"`
	UserName     string   `json:"userName,omitempty"`
	OperatorID   string   `json:"operatorId,omitempty"`
	MessageID    string   `json:"messageId,omitempty"`
	ThreadID     string   `json:"threadId,omitempty"`
}

func botInboxExtra(msg InboundMessage) map[string]string {
	data, err := json.Marshal(durableBotMessage{
		Platform: msg.Platform, ConnectionID: msg.ConnectionID, Domain: msg.Domain,
		ChatType: msg.ChatType, ChatID: msg.ChatID, UserID: msg.UserID,
		UserName: msg.UserName, OperatorID: msg.OperatorID, MessageID: msg.MessageID,
		ThreadID: msg.ThreadID,
	})
	if err != nil {
		return nil
	}
	return map[string]string{botInboxMessageExtraKey: string(data)}
}

func botMessageFromEnvelope(env sessioninbox.PromptEnvelope, fallback InboundMessage) InboundMessage {
	msg := fallback
	if raw := env.Extra[botInboxMessageExtraKey]; raw != "" {
		var stored durableBotMessage
		if json.Unmarshal([]byte(raw), &stored) == nil {
			msg.Platform = stored.Platform
			msg.ConnectionID = stored.ConnectionID
			msg.Domain = stored.Domain
			msg.ChatType = stored.ChatType
			msg.ChatID = stored.ChatID
			msg.UserID = stored.UserID
			msg.UserName = stored.UserName
			msg.OperatorID = stored.OperatorID
			msg.MessageID = stored.MessageID
			msg.ThreadID = stored.ThreadID
		}
	}
	msg.Text = firstNonEmptyBotText(env.DisplayText, env.SubmitText, env.RawText)
	msg.Media = nil
	msg.MediaURLs = nil
	msg.ResolveUserName = nil
	msg.Raw = nil
	return msg
}

func firstNonEmptyBotText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// enqueueViaInbox durably queues an inbound message on the controller's
// session inbox. Platform message IDs are used as idempotency keys.
func enqueueViaInbox(ctrl control.SessionAPI, msg InboundMessage, intent sessioninbox.InboxIntent) (sessioninbox.InboxReceipt, error) {
	if ctrl == nil {
		return sessioninbox.InboxReceipt{}, fmt.Errorf("no controller")
	}
	if ensurer, ok := ctrl.(interface{ EnsureSessionPath() }); ok {
		ensurer.EnsureSessionPath()
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrEmpty
	}
	idem := strings.TrimSpace(msg.MessageID)
	req := control.InboxRequest{
		Intent:      intent,
		Display:     text,
		Raw:         text,
		Submit:      text,
		Source:      "bot",
		Idempotency: idem,
		Extra:       botInboxExtra(msg),
	}
	if intent == sessioninbox.IntentSteer {
		return ctrl.TryEnqueueAndSteer(req)
	}
	// Bot owns synchronous response rendering and drains the durable FIFO itself;
	// detached Controller dispatch would lose the platform sink.
	return ctrl.EnqueueInbox(req)
}

// collectAppend tries to append text into the last queued follow-up blob within
// the debounce window. Falls back to a new enqueue.
func collectAppend(ctrl control.SessionAPI, msg InboundMessage, debounce time.Duration) (sessioninbox.InboxReceipt, error) {
	if ctrl == nil {
		return sessioninbox.InboxReceipt{}, fmt.Errorf("no controller")
	}
	snap := ctrl.InboxSnapshot()
	// Find last queued follow-up.
	var last *sessioninbox.InboxItemMeta
	for i, it := range slices.Backward(snap.Items) {
		if it.State == sessioninbox.StateQueued && it.Intent == sessioninbox.IntentFollowup {
			last = &snap.Items[i]
			break
		}
	}
	text := strings.TrimSpace(msg.Text)
	if last != nil && debounce > 0 && time.Since(last.UpdatedAt) < debounce {
		if _, err := ctrl.AppendInboxItem(last.ID, text, strings.TrimSpace(msg.MessageID), botInboxExtra(msg)); err == nil {
			return sessioninbox.InboxReceipt{
				ItemID:      last.ID,
				Disposition: sessioninbox.DispositionQueuedFollowup,
				Position:    snap.Capacity.Items,
				Paused:      snap.Paused,
				Capacity:    snap.Capacity,
			}, nil
		}
	}
	return enqueueViaInbox(ctrl, msg, sessioninbox.IntentFollowup)
}

// interruptEnqueue cancels the current turn and moves a new item to the front.
func interruptEnqueue(ctrl control.SessionAPI, msg InboundMessage) (sessioninbox.InboxReceipt, error) {
	if ctrl == nil {
		return sessioninbox.InboxReceipt{}, fmt.Errorf("no controller")
	}
	ctrl.Cancel()
	rec, err := enqueueViaInbox(ctrl, msg, sessioninbox.IntentFollowup)
	if err != nil {
		return rec, err
	}
	// Move to front (index 0) so it runs next; do not delete existing queue.
	if err := ctrl.MoveInboxItem(rec.ItemID, 0); err != nil {
		slog.Warn("bot: move interrupt item to front", "err", err)
	}
	return rec, nil
}

// formatQueuedReceipt is the user-visible durable queue confirmation.
func formatQueuedReceipt(rec sessioninbox.InboxReceipt) string {
	return fmt.Sprintf("已持久排队 #%s", shortItemID(rec.ItemID))
}

func shortItemID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// warnDeprecatedQueueDrop logs once when an old drop policy is still configured.
func warnDeprecatedQueueDrop(drop string) {
	switch NormalizeQueueDrop(drop) {
	case QueueDropOld, QueueDropSummarize:
		slog.Warn("bot: queue_drop is deprecated; capacity rejections no longer drop old messages", "drop", drop)
	}
}

// handleQueueInboxCommand extends /queue with durable inbox management.
// Returns handled=false for mode-switch forms of /queue.
func (gw *BotGateway) handleQueueInboxCommand(ctx context.Context, key string, msg InboundMessage) (string, bool, bool) {
	_ = ctx
	parts := strings.Fields(msg.Text)
	if len(parts) < 2 {
		return "", false, false
	}
	sub := strings.ToLower(parts[1])
	if !isBotInboxCommand(sub) {
		return "", false, false
	}
	api := gw.sessionAPI(key)
	if api == nil {
		return "当前没有可管理的会话队列。", true, false
	}
	// Group chats: only the same session initiator or admins may read bodies.
	// Mode-level admin gate is enforced by requireCommandRole on sensitive ops.
	switch sub {
	case "list", "ls":
		return formatBotInboxList(api), true, false
	case "show":
		return showBotInboxItem(api, parts), true, false
	case "delete", "rm":
		return deleteBotInboxItem(api, parts), true, false
	case "move":
		return moveBotInboxItem(api, parts), true, false
	case "pause":
		return setBotInboxPaused(api, true), true, false
	case "resume":
		reply := setBotInboxPaused(api, false)
		return reply, true, reply == "inbox resumed"
	case "retry":
		reply := retryBotInboxItem(api, parts)
		return reply, true, strings.HasPrefix(reply, "retry #")
	case "refresh":
		return refreshBotInboxItem(api, parts), true, false
	}
	return "", false, false
}

func formatBotInboxList(api control.SessionAPI) string {
	snap := api.InboxSnapshot()
	if len(snap.Items) == 0 {
		return "inbox empty" + pausedSuffix(snap.Paused)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "inbox items=%d", len(snap.Items))
	if snap.Paused {
		b.WriteString(" paused")
	}
	b.WriteByte('\n')
	limit := min(len(snap.Items), 15)
	for i := range limit {
		it := snap.Items[i]
		fmt.Fprintf(&b, "%d. [%s/%s] %s #%s\n", i+1, it.Intent, it.State, it.Preview, shortItemID(it.ID))
	}
	return strings.TrimRight(b.String(), "\n")
}

func showBotInboxItem(api control.SessionAPI, parts []string) string {
	if len(parts) < 3 {
		return "用法: /queue show <n|id>"
	}
	id, err := resolveBotInboxRef(api, parts[2])
	if err != nil {
		return err.Error()
	}
	_, env, err := api.ReadInboxItem(id)
	if err != nil {
		return "show: " + err.Error()
	}
	return env.SubmitText
}

func deleteBotInboxItem(api control.SessionAPI, parts []string) string {
	if len(parts) < 3 {
		return "用法: /queue delete <n|id>"
	}
	id, err := resolveBotInboxRef(api, parts[2])
	if err != nil {
		return err.Error()
	}
	if err := api.DeleteInboxItem(id); err != nil {
		return "delete: " + err.Error()
	}
	return "deleted #" + shortItemID(id)
}

func moveBotInboxItem(api control.SessionAPI, parts []string) string {
	if len(parts) < 4 {
		return "用法: /queue move <n|id> <to>"
	}
	id, err := resolveBotInboxRef(api, parts[2])
	if err != nil {
		return err.Error()
	}
	var to int
	if _, err := fmt.Sscanf(parts[3], "%d", &to); err != nil {
		return "move: bad index"
	}
	if err := api.MoveInboxItem(id, to-1); err != nil {
		return "move: " + err.Error()
	}
	return "moved #" + shortItemID(id)
}

func setBotInboxPaused(api control.SessionAPI, paused bool) string {
	setter, ok := any(api).(interface{ SetInboxPausedPassive(bool) error })
	var err error
	if ok {
		err = setter.SetInboxPausedPassive(paused)
	} else {
		err = api.SetInboxPaused(paused)
	}
	if err != nil {
		return err.Error()
	}
	if paused {
		return "inbox paused"
	}
	return "inbox resumed"
}

func retryBotInboxItem(api control.SessionAPI, parts []string) string {
	if len(parts) < 3 {
		return "用法: /queue retry <n|id>"
	}
	id, err := resolveBotInboxRef(api, parts[2])
	if err != nil {
		return err.Error()
	}
	retrier, ok := any(api).(interface{ RetryInboxItemPassive(string) error })
	var retryErr error
	if ok {
		retryErr = retrier.RetryInboxItemPassive(id)
	} else {
		retryErr = api.RetryInboxItem(id)
	}
	if retryErr != nil {
		return retryErr.Error()
	}
	return "retry #" + shortItemID(id)
}

func refreshBotInboxItem(api control.SessionAPI, parts []string) string {
	if len(parts) < 3 {
		return "用法: /queue refresh <n|id>"
	}
	id, err := resolveBotInboxRef(api, parts[2])
	if err != nil {
		return err.Error()
	}
	if err := api.RefreshInboxReferences(id); err != nil {
		return err.Error()
	}
	return "refs refreshed #" + shortItemID(id)
}

func isBotInboxCommand(sub string) bool {
	switch sub {
	case "list", "ls", "show", "delete", "rm", "move", "pause", "resume", "retry", "refresh":
		return true
	default:
		return false
	}
}

func pausedSuffix(paused bool) string {
	if paused {
		return " (paused)"
	}
	return ""
}

func resolveBotInboxRef(api control.SessionAPI, ref string) (string, error) {
	snap := api.InboxSnapshot()
	var n int
	if _, err := fmt.Sscanf(ref, "%d", &n); err == nil && n >= 1 && n <= len(snap.Items) {
		return snap.Items[n-1].ID, nil
	}
	for _, it := range snap.Items {
		if it.ID == ref || strings.HasPrefix(it.ID, ref) {
			return it.ID, nil
		}
	}
	return "", fmt.Errorf("unknown inbox item %q", ref)
}
