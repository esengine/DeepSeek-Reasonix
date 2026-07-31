package channel

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func triggerMessage(d *dispatcher.EventDispatcher, eventID, messageID string, createTimeMs int64) {
	payload := fmt.Sprintf(`{
		"schema": "2.0",
		"header": {
			"event_id": "%s",
			"event_type": "im.message.receive_v1",
			"create_time": "%d"
		},
		"event": {
			"sender": {
				"sender_id": {
					"open_id": "ou_sender_123"
				}
			},
			"message": {
				"message_id": "%s",
				"chat_id": "oc_test_chat",
				"chat_type": "group",
				"message_type": "text",
				"content": "{\"text\":\"hello\"}"
			}
		}
	}`, eventID, createTimeMs, messageID)

	req := &larkevent.EventReq{
		Header: http.Header{},
		Body:   []byte(payload),
	}
	_ = d.Handle(context.Background(), req)
}

func TestMessageDedupUsesMessageID(t *testing.T) {
	d := dispatcher.NewEventDispatcher("", "")
	wsCli := larkws.NewClient("appID", "appSecret", larkws.WithEventHandler(d))
	cli := lark.NewClient("appID", "appSecret")
	requireMention := false
	safetyCfg := types.DefaultChannelConfig().Safety
	safetyCfg.Batch.DelayMs = 10 * time.Millisecond
	safetyCfg.Batch.LongDelayMs = 10 * time.Millisecond

	ch := NewChannel(
		cli,
		wsCli,
		types.WithSafetyConfig(safetyCfg),
		types.WithPolicyConfig(types.PolicyConfig{
			RequireMention: &requireMention,
		}),
	)
	impl := ch.(*channelImpl)

	// Avoid fetching bot identity over the network during tests.
	impl.botIdentity = &types.BotIdentity{
		OpenID: "ou_bot",
		Name:   "test-bot",
	}

	var handled int32
	ch.OnMessage(func(ctx context.Context, msg *types.NormalizedMessage) error {
		atomic.AddInt32(&handled, 1)
		return nil
	})

	nowMs := time.Now().UnixMilli()
	triggerMessage(d, "evt_1", "om_same_123", nowMs)
	time.Sleep(80 * time.Millisecond)

	if got := atomic.LoadInt32(&handled); got != 1 {
		t.Fatalf("expected first message to be handled once, got %d", got)
	}

	// Same message_id with a different event_id should be treated as duplicate.
	triggerMessage(d, "evt_2", "om_same_123", nowMs+1)
	time.Sleep(80 * time.Millisecond)

	if got := atomic.LoadInt32(&handled); got != 1 {
		t.Fatalf("expected duplicate message_id to be dropped, got %d handler calls", got)
	}
}
