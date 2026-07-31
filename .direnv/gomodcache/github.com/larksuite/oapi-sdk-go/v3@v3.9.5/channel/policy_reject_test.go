package channel

import (
	"context"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func TestChannelUpdatePolicyAndGetPolicy(t *testing.T) {
	d := dispatcher.NewEventDispatcher("", "")
	wsCli := larkws.NewClient("appID", "appSecret", larkws.WithEventHandler(d))
	cli := lark.NewClient("appID", "appSecret")

	ch := NewChannel(cli, wsCli)

	requireMention := false
	respondAll := true
	cfg := types.PolicyConfig{
		DMMode:              "allowlist",
		DMAllowlist:         []string{"ou_user_1"},
		GroupAllowlist:      []string{"oc_group_1"},
		RequireMention:      &requireMention,
		RespondToMentionAll: &respondAll,
	}

	ch.UpdatePolicy(cfg)
	got := ch.GetPolicy()

	if got.DMMode != cfg.DMMode {
		t.Fatalf("expected DMMode %s, got %s", cfg.DMMode, got.DMMode)
	}
	if len(got.DMAllowlist) != 1 || got.DMAllowlist[0] != "ou_user_1" {
		t.Fatalf("expected DMAllowlist to round-trip, got %#v", got.DMAllowlist)
	}
	if len(got.GroupAllowlist) != 1 || got.GroupAllowlist[0] != "oc_group_1" {
		t.Fatalf("expected GroupAllowlist to round-trip, got %#v", got.GroupAllowlist)
	}
	if got.RequireMention == nil || *got.RequireMention != requireMention {
		t.Fatalf("expected RequireMention=%v, got %#v", requireMention, got.RequireMention)
	}
	if got.RespondToMentionAll == nil || *got.RespondToMentionAll != respondAll {
		t.Fatalf("expected RespondToMentionAll=%v, got %#v", respondAll, got.RespondToMentionAll)
	}
}

func TestChannelOnRejectEmitsRejectEvent(t *testing.T) {
	d := dispatcher.NewEventDispatcher("", "")
	wsCli := larkws.NewClient("appID", "appSecret", larkws.WithEventHandler(d))
	cli := lark.NewClient("appID", "appSecret")

	requireMention := true
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
	impl.botIdentity = &types.BotIdentity{
		OpenID: "ou_bot",
		Name:   "test-bot",
	}

	// Ensure message events are wired into the dispatcher.
	ch.OnMessage(func(ctx context.Context, msg *types.NormalizedMessage) error {
		return nil
	})

	rejectCh := make(chan *types.RejectEvent, 1)
	ch.OnReject(func(ctx context.Context, event *types.RejectEvent) error {
		rejectCh <- event
		return nil
	})

	triggerMessage(d, "evt_reject_1", "om_reject_1", time.Now().UnixMilli())

	select {
	case event := <-rejectCh:
		if event.MessageID != "om_reject_1" {
			t.Fatalf("expected MessageID om_reject_1, got %s", event.MessageID)
		}
		if event.ChatID != "oc_test_chat" {
			t.Fatalf("expected ChatID oc_test_chat, got %s", event.ChatID)
		}
		if event.SenderID != "ou_sender_123" {
			t.Fatalf("expected SenderID ou_sender_123, got %s", event.SenderID)
		}
		if event.Reason != string(types.RejectReasonNoMention) {
			t.Fatalf("expected reason %s, got %s", types.RejectReasonNoMention, event.Reason)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected reject event to be emitted")
	}
}
