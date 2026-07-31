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

func triggerCardAction(d *dispatcher.EventDispatcher, eventID, messageID, chatID string) {
	payload := fmt.Sprintf(`{
		"schema": "2.0",
		"header": {
			"event_id": "%s",
			"event_type": "card.action.trigger",
			"create_time": "1700000000000"
		},
		"event": {
			"operator": {
				"open_id": "ou_card_actor_123",
				"user_id": "u_card_actor_123",
				"tenant_key": "tenant_123"
			},
			"token": "card_token_123",
			"host": "im_message",
			"delivery_type": "url_preview",
			"context": {
				"url": "https://example.com/card",
				"preview_token": "preview_123",
				"open_message_id": "%s",
				"open_chat_id": "%s"
			},
			"action": {
				"value": {"approve": true},
				"tag": "button",
				"name": "approve",
				"option": "primary",
				"input_value": "hello"
			}
		}
	}`, eventID, messageID, chatID)

	req := &larkevent.EventReq{
		Header: http.Header{},
		Body:   []byte(payload),
	}
	_ = d.Handle(context.Background(), req)
}

func TestOnCardActionUsesCardActionEvent(t *testing.T) {
	d := dispatcher.NewEventDispatcher("", "")
	wsCli := larkws.NewClient("appID", "appSecret", larkws.WithEventHandler(d))
	cli := lark.NewClient("appID", "appSecret")
	ch := NewChannel(cli, wsCli)

	var handled int32
	done := make(chan *types.CardActionEvent, 1)

	ch.OnCardAction(func(ctx context.Context, event *types.CardActionEvent) error {
		atomic.AddInt32(&handled, 1)
		done <- event
		return nil
	})

	triggerCardAction(d, "evt_card_1", "om_card_1", "oc_card_1")

	select {
	case event := <-done:
		if event.MessageID != "om_card_1" {
			t.Fatalf("expected MessageID om_card_1, got %s", event.MessageID)
		}
		if event.ChatID != "oc_card_1" {
			t.Fatalf("expected ChatID oc_card_1, got %s", event.ChatID)
		}
		if event.Operator.OpenID != "ou_card_actor_123" {
			t.Fatalf("expected Operator.OpenID ou_card_actor_123, got %s", event.Operator.OpenID)
		}
		if event.Action.Tag != "button" {
			t.Fatalf("expected Action.Tag button, got %s", event.Action.Tag)
		}
		if event.Action.Name != "approve" {
			t.Fatalf("expected Action.Name approve, got %s", event.Action.Name)
		}
		if event.Action.InputValue != "hello" {
			t.Fatalf("expected Action.InputValue hello, got %s", event.Action.InputValue)
		}
		if approved, ok := event.Action.Value["approve"].(bool); !ok || !approved {
			t.Fatalf("expected Action.Value[approve] = true, got %#v", event.Action.Value["approve"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected card action handler to be invoked")
	}

	if got := atomic.LoadInt32(&handled); got != 1 {
		t.Fatalf("expected card action handler to run once, got %d", got)
	}
}
