package normalize

import (
	"encoding/json"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestParseMessage(t *testing.T) {
	msgType := "text"
	content := `{"text":"hello world @_all"}`
	msgId := "om_123"
	chatId := "oc_456"
	userId := "ou_789"
	mentionUserId := "ou_abc"
	mentionOpenId := "ou_open_abc"
	mentionName := "Alice"
	mentionKey := "@_user_1"
	allKey := "@_all"

	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					UserId: &userId,
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   &msgId,
				ChatId:      &chatId,
				MessageType: &msgType,
				Content:     &content,
				Mentions: []*larkim.MentionEvent{
					{
						Key: &mentionKey,
						Id: &larkim.UserId{
							UserId: &mentionUserId,
							OpenId: &mentionOpenId,
						},
						Name: &mentionName,
					},
					{
						Key: &allKey,
					},
				},
			},
		},
	}

	norm := ParseMessage(event)
	if norm == nil {
		t.Fatal("expected norm to not be nil")
	}

	if norm.MessageID != msgId {
		t.Errorf("expected MessageID %s, got %s", msgId, norm.MessageID)
	}
	if norm.ChatID != chatId {
		t.Errorf("expected ChatID %s, got %s", chatId, norm.ChatID)
	}
	if norm.UserID != userId {
		t.Errorf("expected UserID %s, got %s", userId, norm.UserID)
	}
	if norm.Content != "hello world @_all" {
		t.Errorf("expected Content 'hello world @_all', got %s", norm.Content)
	}
	if norm.RawContentType != "text" {
		t.Errorf("expected RawContentType 'text', got %s", norm.RawContentType)
	}
	if !norm.MentionAll {
		t.Errorf("expected MentionAll to be true")
	}
	if len(norm.Mentions) != 2 {
		t.Fatalf("expected 2 mentions, got %d", len(norm.Mentions))
	}

	// Test first mention (Alice)
	if norm.Mentions[0].UserID != mentionUserId {
		t.Errorf("expected mention UserID %s, got %s", mentionUserId, norm.Mentions[0].UserID)
	}
	if norm.Mentions[0].OpenID != mentionOpenId {
		t.Errorf("expected mention OpenID %s, got %s", mentionOpenId, norm.Mentions[0].OpenID)
	}
	if norm.Mentions[0].Key != mentionKey {
		t.Errorf("expected mention Key %s, got %s", mentionKey, norm.Mentions[0].Key)
	}
}

func TestParseCardAction(t *testing.T) {
	val := map[string]interface{}{"key": "value"}
	formVal := map[string]interface{}{"field": "form"}
	userID := "u_123"
	tenantKey := "tenant_123"
	event := &callback.CardActionTriggerEvent{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID: "event_card_123",
			},
		},
		Event: &callback.CardActionTriggerRequest{
			Context: &callback.Context{
				URL:           "https://example.com",
				PreviewToken:  "preview_123",
				OpenMessageID: "om_123",
				OpenChatID:    "oc_456",
			},
			Operator: &callback.Operator{
				OpenID:    "ou_789",
				UserID:    &userID,
				TenantKey: &tenantKey,
			},
			Token:        "card_token_123",
			Host:         "im_message",
			DeliveryType: "url_preview",
			Action: &callback.CallBackAction{
				Value:      val,
				Tag:        "button",
				Name:       "approve",
				Option:     "A",
				Timezone:   "Asia/Shanghai",
				FormValue:  formVal,
				InputValue: "input_123",
				Options:    []string{"opt1", "opt2"},
				Checked:    true,
			},
		},
	}

	norm := ParseCardAction(event)
	if norm == nil {
		t.Fatal("expected norm to not be nil")
	}

	if norm.EventID != "event_card_123" {
		t.Errorf("expected EventID event_card_123, got %s", norm.EventID)
	}
	if norm.MessageID != "om_123" {
		t.Errorf("expected MessageID om_123, got %s", norm.MessageID)
	}
	if norm.ChatID != "oc_456" {
		t.Errorf("expected ChatID oc_456, got %s", norm.ChatID)
	}
	if norm.Token != "card_token_123" {
		t.Errorf("expected Token card_token_123, got %s", norm.Token)
	}
	if norm.Host != "im_message" {
		t.Errorf("expected Host im_message, got %s", norm.Host)
	}
	if norm.DeliveryType != "url_preview" {
		t.Errorf("expected DeliveryType url_preview, got %s", norm.DeliveryType)
	}
	if norm.Operator.OpenID != "ou_789" {
		t.Errorf("expected Operator.OpenID ou_789, got %s", norm.Operator.OpenID)
	}
	if norm.Operator.UserID != userID {
		t.Errorf("expected Operator.UserID %s, got %s", userID, norm.Operator.UserID)
	}
	if norm.Operator.TenantKey != tenantKey {
		t.Errorf("expected Operator.TenantKey %s, got %s", tenantKey, norm.Operator.TenantKey)
	}
	if norm.Action.Tag != "button" {
		t.Errorf("expected Action.Tag button, got %s", norm.Action.Tag)
	}
	if norm.Action.Name != "approve" {
		t.Errorf("expected Action.Name approve, got %s", norm.Action.Name)
	}
	if norm.Action.Option != "A" {
		t.Errorf("expected Action.Option A, got %s", norm.Action.Option)
	}
	if norm.Action.Timezone != "Asia/Shanghai" {
		t.Errorf("expected Action.Timezone Asia/Shanghai, got %s", norm.Action.Timezone)
	}
	if norm.Action.InputValue != "input_123" {
		t.Errorf("expected Action.InputValue input_123, got %s", norm.Action.InputValue)
	}
	if !norm.Action.Checked {
		t.Errorf("expected Action.Checked true")
	}
	expectedValue, _ := json.Marshal(val)
	actualValue, _ := json.Marshal(norm.Action.Value)
	if string(actualValue) != string(expectedValue) {
		t.Errorf("expected Action.Value %s, got %s", string(expectedValue), string(actualValue))
	}
	expectedFormValue, _ := json.Marshal(formVal)
	actualFormValue, _ := json.Marshal(norm.Action.FormValue)
	if string(actualFormValue) != string(expectedFormValue) {
		t.Errorf("expected Action.FormValue %s, got %s", string(expectedFormValue), string(actualFormValue))
	}
	if len(norm.Action.Options) != 2 || norm.Action.Options[0] != "opt1" || norm.Action.Options[1] != "opt2" {
		t.Errorf("expected Action.Options [opt1 opt2], got %#v", norm.Action.Options)
	}
	if norm.Context.URL != "https://example.com" {
		t.Errorf("expected Context.URL https://example.com, got %s", norm.Context.URL)
	}
	if norm.Context.PreviewToken != "preview_123" {
		t.Errorf("expected Context.PreviewToken preview_123, got %s", norm.Context.PreviewToken)
	}
}

func TestParseReaction(t *testing.T) {
	eventId := "event_123"
	createTime := "1610000000000"
	msgId := "om_123"
	userId := "ou_789"
	reactionType := "SMILE"
	operatorType := "user"

	action := "added"

	eventPayload := &larkim.P2MessageReactionCreatedV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    eventId,
				CreateTime: createTime,
			},
		},
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    &msgId,
			OperatorType: &operatorType,
			UserId: &larkim.UserId{
				UserId: &userId,
			},
			ReactionType: &larkim.Emoji{
				EmojiType: &reactionType,
			},
			ActionTime: &createTime,
		},
	}

	norm := ParseReaction(eventPayload)
	if norm == nil {
		t.Fatal("expected norm to not be nil")
	}

	if norm.EventID != eventId {
		t.Errorf("expected EventID %s, got %s", eventId, norm.EventID)
	}
	if norm.MessageID != msgId {
		t.Errorf("expected MessageID %s, got %s", msgId, norm.MessageID)
	}
	if norm.OperatorType != operatorType {
		t.Errorf("expected OperatorType %s, got %s", operatorType, norm.OperatorType)
	}
	if norm.UserID != userId {
		t.Errorf("expected UserID %s, got %s", userId, norm.UserID)
	}
	if norm.ReactionType != reactionType {
		t.Errorf("expected ReactionType %s, got %s", reactionType, norm.ReactionType)
	}
	if norm.Action != action {
		t.Errorf("expected Action %s, got %s", action, norm.Action)
	}
}

func TestParseReaction_AppOperatorFallsBackToAppID(t *testing.T) {
	eventId := "event_456"
	createTime := "1610000000001"
	msgId := "om_456"
	appID := "cli_test_app"
	reactionType := "THUMBSUP"
	operatorType := "app"

	eventPayload := &larkim.P2MessageReactionCreatedV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    eventId,
				CreateTime: createTime,
			},
		},
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    &msgId,
			OperatorType: &operatorType,
			AppId:        &appID,
			ReactionType: &larkim.Emoji{
				EmojiType: &reactionType,
			},
			ActionTime: &createTime,
		},
	}

	norm := ParseReaction(eventPayload)
	if norm == nil {
		t.Fatal("expected norm to not be nil")
	}

	if norm.UserID != appID {
		t.Fatalf("expected UserID fallback to AppID %s, got %s", appID, norm.UserID)
	}
	if norm.OperatorType != operatorType {
		t.Fatalf("expected OperatorType %s, got %s", operatorType, norm.OperatorType)
	}
	if norm.ReactionType != reactionType {
		t.Fatalf("expected ReactionType %s, got %s", reactionType, norm.ReactionType)
	}
	if norm.Action != "added" {
		t.Fatalf("expected Action added, got %s", norm.Action)
	}
}
