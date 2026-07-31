package normalize

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// ParseMessage normalizes a P2MessageReceiveV1 event.
func ParseMessage(event *larkim.P2MessageReceiveV1) *types.NormalizedMessage {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	msg := event.Event.Message
	sender := event.Event.Sender

	norm := &types.NormalizedMessage{
		RawEvent: event,
	}

	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		norm.EventID = event.EventV2Base.Header.EventID
		if event.EventV2Base.Header.CreateTime != "" {
			if ms, err := strconv.ParseInt(event.EventV2Base.Header.CreateTime, 10, 64); err == nil {
				norm.CreateTimeMs = ms
			}
		}
	}

	if msg.MessageId != nil {
		norm.MessageID = *msg.MessageId
	}
	if msg.ChatId != nil {
		norm.ChatID = *msg.ChatId
	}
	if msg.ChatType != nil {
		norm.ChatType = *msg.ChatType
	}
	if sender != nil && sender.SenderId != nil {
		if sender.SenderId.OpenId != nil {
			norm.UserID = *sender.SenderId.OpenId
		} else if sender.SenderId.UserId != nil {
			norm.UserID = *sender.SenderId.UserId
		}
	}
	if msg.Mentions != nil {
		for _, m := range msg.Mentions {
			mention := types.Mention{}
			if m.Key != nil {
				mention.Key = *m.Key
			}
			if m.Id != nil {
				if m.Id.UserId != nil {
					mention.UserID = *m.Id.UserId
				}
				if m.Id.OpenId != nil {
					mention.OpenID = *m.Id.OpenId
					// Backwards compatibility for UserID field if it's empty
					if mention.UserID == "" {
						mention.UserID = *m.Id.OpenId
					}
				}
			}
			if m.Name != nil {
				mention.Name = *m.Name
			}
			norm.Mentions = append(norm.Mentions, mention)

			if m.Key != nil && (*m.Key == "@_all" || *m.Key == "@all") {
				norm.MentionAll = true
			}
		}
	}

	// Extract content and resources
	if msg.Content != nil && msg.MessageType != nil {
		norm.RawContentType = *msg.MessageType
		content, resources := ParseContent(*msg.MessageType, *msg.Content)
		if content != "" {
			norm.Content = content
			if strings.Contains(content, "@_all") || strings.Contains(content, "@all") {
				norm.MentionAll = true
			}
		}
		if len(resources) > 0 {
			norm.Resources = append(norm.Resources, resources...)
		}
	}

	return norm
}

// ParseReaction normalizes a message reaction event (Created or Deleted).
func ParseReaction(event interface{}) *types.ReactionEvent {
	norm := &types.ReactionEvent{
		RawEvent: event,
	}

	switch ev := event.(type) {
	case *larkim.P2MessageReactionCreatedV1:
		if ev.EventV2Base != nil && ev.EventV2Base.Header != nil {
			norm.EventID = ev.EventV2Base.Header.EventID
			if ev.EventV2Base.Header.CreateTime != "" {
				if ms, err := strconv.ParseInt(ev.EventV2Base.Header.CreateTime, 10, 64); err == nil {
					norm.CreateTimeMs = ms
				}
			}
		}
		if ev.Event != nil {
			if ev.Event.MessageId != nil {
				norm.MessageID = *ev.Event.MessageId
			}
			if ev.Event.OperatorType != nil {
				norm.OperatorType = *ev.Event.OperatorType
			}
			if ev.Event.ReactionType != nil && ev.Event.ReactionType.EmojiType != nil {
				norm.ReactionType = *ev.Event.ReactionType.EmojiType
			}
			if ev.Event.UserId != nil {
				if ev.Event.UserId.OpenId != nil {
					norm.UserID = *ev.Event.UserId.OpenId
				} else if ev.Event.UserId.UserId != nil {
					norm.UserID = *ev.Event.UserId.UserId
				}
			}
			if norm.UserID == "" && ev.Event.AppId != nil {
				norm.UserID = *ev.Event.AppId
			}
			norm.Action = "added"
		}

	case *larkim.P2MessageReactionDeletedV1:
		if ev.EventV2Base != nil && ev.EventV2Base.Header != nil {
			norm.EventID = ev.EventV2Base.Header.EventID
			if ev.EventV2Base.Header.CreateTime != "" {
				if ms, err := strconv.ParseInt(ev.EventV2Base.Header.CreateTime, 10, 64); err == nil {
					norm.CreateTimeMs = ms
				}
			}
		}
		if ev.Event != nil {
			if ev.Event.MessageId != nil {
				norm.MessageID = *ev.Event.MessageId
			}
			if ev.Event.OperatorType != nil {
				norm.OperatorType = *ev.Event.OperatorType
			}
			if ev.Event.ReactionType != nil && ev.Event.ReactionType.EmojiType != nil {
				norm.ReactionType = *ev.Event.ReactionType.EmojiType
			}
			if ev.Event.UserId != nil {
				if ev.Event.UserId.OpenId != nil {
					norm.UserID = *ev.Event.UserId.OpenId
				} else if ev.Event.UserId.UserId != nil {
					norm.UserID = *ev.Event.UserId.UserId
				}
			}
			if norm.UserID == "" && ev.Event.AppId != nil {
				norm.UserID = *ev.Event.AppId
			}
			norm.Action = "removed"
		}
	default:
		return nil
	}

	return norm
}

// ParseComment normalizes a drive.notice.comment_add_v1 event.
func ParseComment(event *larkevent.EventReq) *types.CommentEvent {
	if event == nil || len(event.Body) == 0 {
		return nil
	}

	var payload struct {
		Header struct {
			EventID    string `json:"event_id"`
			CreateTime string `json:"create_time"`
		} `json:"header"`
		Event struct {
			CommentID   string `json:"comment_id"`
			IsMentioned *bool  `json:"is_mentioned"`
			IsMention   *bool  `json:"is_mention"`
			ReplyID     string `json:"reply_id"`
			FileToken   string `json:"file_token"`
			FileType    string `json:"file_type"`
			CreateTime  string `json:"create_time"`
			ActionTime  string `json:"action_time"`
			UserID      *struct {
				UserID  string `json:"user_id"`
				OpenID  string `json:"open_id"`
				UnionID string `json:"union_id"`
			} `json:"user_id"`
			NoticeMeta *struct {
				FileToken   string `json:"file_token"`
				FileType    string `json:"file_type"`
				Timestamp   string `json:"timestamp"`
				IsMentioned *bool  `json:"is_mentioned"`
				FromUserID  *struct {
					UserID  string `json:"user_id"`
					OpenID  string `json:"open_id"`
					UnionID string `json:"union_id"`
				} `json:"from_user_id"`
			} `json:"notice_meta"`
		} `json:"event"`
	}

	if err := json.Unmarshal(event.Body, &payload); err != nil {
		return nil
	}

	ev := payload.Event

	fileToken := ev.FileToken
	if fileToken == "" && ev.NoticeMeta != nil {
		fileToken = ev.NoticeMeta.FileToken
	}

	fileType := ev.FileType
	if fileType == "" && ev.NoticeMeta != nil {
		fileType = ev.NoticeMeta.FileType
	}

	commentID := ev.CommentID

	var operatorOpenId, operatorUserId, operatorUnionId string
	if ev.NoticeMeta != nil && ev.NoticeMeta.FromUserID != nil {
		operatorOpenId = ev.NoticeMeta.FromUserID.OpenID
		operatorUserId = ev.NoticeMeta.FromUserID.UserID
		operatorUnionId = ev.NoticeMeta.FromUserID.UnionID
	} else if ev.UserID != nil {
		operatorOpenId = ev.UserID.OpenID
		operatorUserId = ev.UserID.UserID
		operatorUnionId = ev.UserID.UnionID
	}

	if fileToken == "" || fileType == "" || commentID == "" || operatorOpenId == "" {
		return nil
	}

	tsStr := ev.CreateTime
	if tsStr == "" && ev.NoticeMeta != nil {
		tsStr = ev.NoticeMeta.Timestamp
	}
	if tsStr == "" {
		tsStr = ev.ActionTime
	}
	if tsStr == "" {
		tsStr = payload.Header.CreateTime
	}

	var timestamp int64
	if ms, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
		timestamp = ms
	} else {
		timestamp = 0
	}

	mentionedBot := false
	if ev.IsMentioned != nil {
		mentionedBot = *ev.IsMentioned
	} else if ev.NoticeMeta != nil && ev.NoticeMeta.IsMentioned != nil {
		mentionedBot = *ev.NoticeMeta.IsMentioned
	} else if ev.IsMention != nil {
		mentionedBot = *ev.IsMention
	}

	norm := &types.CommentEvent{
		RawEvent:  event,
		EventID:   payload.Header.EventID,
		CommentID: commentID,
		FileToken: fileToken,
		FileType:  fileType,
		ReplyID:   ev.ReplyID,
		Operator: types.OperatorInfo{
			OpenID:  operatorOpenId,
			UserID:  operatorUserId,
			UnionID: operatorUnionId,
		},
		MentionedBot: mentionedBot,
		Timestamp:    timestamp,
	}

	return norm
}

// ParseBotAdded normalizes a P2ChatMemberBotAddedV1 event.
func ParseBotAdded(event *larkim.P2ChatMemberBotAddedV1) *types.BotAddedEvent {
	if event == nil || event.Event == nil {
		return nil
	}

	norm := &types.BotAddedEvent{
		RawEvent: event,
	}

	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		norm.EventID = event.EventV2Base.Header.EventID
		if event.EventV2Base.Header.CreateTime != "" {
			if ms, err := strconv.ParseInt(event.EventV2Base.Header.CreateTime, 10, 64); err == nil {
				norm.CreateTimeMs = ms
			}
		}
	}

	if event.Event.ChatId != nil {
		norm.ChatID = *event.Event.ChatId
	}
	if event.Event.Name != nil {
		norm.ChatName = *event.Event.Name
	}
	if event.Event.OperatorId != nil {
		if event.Event.OperatorId.OpenId != nil {
			norm.UserID = *event.Event.OperatorId.OpenId
		} else if event.Event.OperatorId.UserId != nil {
			norm.UserID = *event.Event.OperatorId.UserId
		}
	}

	return norm
}

// ParseCardAction normalizes a CardActionTriggerEvent.
func ParseCardAction(event *callback.CardActionTriggerEvent) *types.CardActionEvent {
	if event == nil || event.Event == nil {
		return nil
	}

	req := event.Event
	norm := &types.CardActionEvent{
		RawEvent: event,
	}

	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		norm.EventID = event.EventV2Base.Header.EventID
	}

	norm.Token = req.Token
	norm.Host = req.Host
	norm.DeliveryType = req.DeliveryType

	if req.Context != nil {
		norm.MessageID = req.Context.OpenMessageID
		norm.ChatID = req.Context.OpenChatID
		norm.Context = types.CardActionContext{
			URL:           req.Context.URL,
			PreviewToken:  req.Context.PreviewToken,
			OpenMessageID: req.Context.OpenMessageID,
			OpenChatID:    req.Context.OpenChatID,
		}
	}

	if req.Operator != nil {
		norm.Operator.OpenID = req.Operator.OpenID
		if req.Operator.UserID != nil {
			norm.Operator.UserID = *req.Operator.UserID
		}
		if req.Operator.TenantKey != nil {
			norm.Operator.TenantKey = *req.Operator.TenantKey
		}
	}

	if req.Action != nil {
		norm.Action = types.CardActionPayload{
			Tag:        req.Action.Tag,
			Option:     req.Action.Option,
			Timezone:   req.Action.Timezone,
			Name:       req.Action.Name,
			InputValue: req.Action.InputValue,
			Options:    append([]string(nil), req.Action.Options...),
			Checked:    req.Action.Checked,
		}
		if req.Action.Value != nil {
			norm.Action.Value = req.Action.Value
		}
		if req.Action.FormValue != nil {
			norm.Action.FormValue = req.Action.FormValue
		}
	}

	return norm
}
