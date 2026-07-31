package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func (c *channelImpl) Send(ctx context.Context, input *types.SendInput) (*types.SendResult, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}

	receiveIDType := "open_id"
	receiveID := input.UserID
	if input.ReceiveID != "" {
		receiveID = input.ReceiveID
		t, err := outbound.DetectReceiveIdType(receiveID)
		if err == nil {
			receiveIDType = string(t)
		}
	} else if input.ChatID != "" {
		receiveIDType = "chat_id"
		receiveID = input.ChatID
	}

	if receiveID == "" {
		return nil, fmt.Errorf("ReceiveID, ChatID, or UserID must be provided")
	}

	// 1. Process local files first
	if input.ImagePath != "" && input.ImageKey == "" {
		// Upload image
		imageKey, err := c.uploader.UploadImagePath(ctx, "message", input.ImagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to upload image: %v", err)
		}
		input.ImageKey = imageKey
	}

	if input.FilePath != "" && input.FileKey == "" {
		// Upload file
		fileKey, err := c.uploader.UploadFilePath(ctx, "stream", input.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to upload file: %v", err)
		}
		input.FileKey = fileKey
	}

	if input.Media != nil && input.AudioKey == "" && input.VideoKey == "" && input.FileKey == "" && input.ImageKey == "" {
		res, err := c.uploader.UploadMedia(ctx, input.Media, nil) // Assume default SsrfGuardOptions or nil
		if err != nil {
			return nil, fmt.Errorf("failed to upload media: %v", err)
		}
		if res.Kind == types.MediaKindImage {
			input.ImageKey = res.FileKey
		} else if res.Kind == types.MediaKindAudio {
			input.AudioKey = res.FileKey
		} else if res.Kind == types.MediaKindVideo {
			input.VideoKey = res.FileKey
		} else {
			input.FileKey = res.FileKey
		}
	}

	// 2. Determine MsgType and Content
	msgType := input.MsgType
	var content string

	if input.ImageKey != "" {
		msgType = "image"
		contentMap := map[string]string{"image_key": input.ImageKey}
		b, _ := json.Marshal(contentMap)
		content = string(b)
	} else if input.AudioKey != "" {
		msgType = "audio"
		contentMap := map[string]string{"file_key": input.AudioKey}
		b, _ := json.Marshal(contentMap)
		content = string(b)
	} else if input.VideoKey != "" {
		msgType = "media"
		contentMap := map[string]string{"file_key": input.VideoKey}
		b, _ := json.Marshal(contentMap)
		content = string(b)
	} else if input.FileKey != "" {
		msgType = "file"
		contentMap := map[string]string{"file_key": input.FileKey}
		b, _ := json.Marshal(contentMap)
		content = string(b)
	} else if input.Card != "" {
		msgType = "interactive"
		content = input.Card
	} else if input.Post != "" {
		msgType = "post"
		content = input.Post
	} else if input.ShareChatID != "" {
		msgType = "share_chat"
		contentMap := map[string]string{"chat_id": input.ShareChatID}
		b, _ := json.Marshal(contentMap)
		content = string(b)
	} else if input.ShareUserID != "" {
		msgType = "share_user"
		// 飞书的 share_user 类型里，如果是 ou_ 开头说明是 open_id，通常底层需要显式传 user_id 作为通用键
		// 但为了保险，我们可以把它赋值给 user_id
		contentMap := map[string]string{"user_id": input.ShareUserID}
		b, _ := json.Marshal(contentMap)
		content = string(b)
	} else if input.StickerFileKey != "" {
		msgType = "sticker"
		contentMap := map[string]string{"file_key": input.StickerFileKey}
		b, _ := json.Marshal(contentMap)
		content = string(b)
	} else if input.Markdown != "" {
		msgType = "post"
		// For Markdown, we split by code fences and newlines to avoid breaking syntax
		chunks := outbound.SplitWithCodeFences(input.Markdown, c.config.Outbound.TextChunkLimit)
		var ids []string
		var firstChatID string
		for i, chunk := range chunks {
			var mentions []types.Mention
			if i == 0 {
				mentions = input.Mentions
			}
			postJSON, err := normalize.SimpleMarkdownToPost(input.Title, chunk, mentions)
			if err != nil {
				return nil, fmt.Errorf("failed to format markdown: %v", err)
			}
			id, chatID, err := c.sendOneWithFallback(ctx, receiveIDType, receiveID, "post", postJSON, input)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
			if i == 0 {
				firstChatID = chatID
			}
		}
		res := &types.SendResult{
			MessageID: ids[0],
			ChatID:    firstChatID,
		}
		if len(ids) > 1 {
			res.ChunkIDs = ids
		}
		return res, nil

	} else if input.Text != "" {
		msgType = "text"

		prefix := normalize.ComposeMentionsTextPrefix(input.Mentions)
		fullText := prefix + input.Text

		// For text, we can also split simply by length
		chunks := splitPlain(fullText, c.config.Outbound.TextChunkLimit)
		var ids []string
		var firstChatID string
		for i, chunk := range chunks {
			contentMap := map[string]string{"text": chunk}
			b, _ := json.Marshal(contentMap)
			id, chatID, err := c.sendOneWithFallback(ctx, receiveIDType, receiveID, "text", string(b), input)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
			if i == 0 {
				firstChatID = chatID
			}
		}
		res := &types.SendResult{
			MessageID: ids[0],
			ChatID:    firstChatID,
		}
		if len(ids) > 1 {
			res.ChunkIDs = ids
		}
		return res, nil
	}

	if msgType == "" || content == "" {
		return nil, fmt.Errorf("no valid message content provided")
	}

	// 3. Send single message for other types
	id, chatID, err := c.sendOneWithFallback(ctx, receiveIDType, receiveID, msgType, content, input)
	if err != nil {
		return nil, err
	}

	return &types.SendResult{
		MessageID: id,
		ChatID:    chatID,
	}, nil
}

func (c *channelImpl) sendOneWithFallback(ctx context.Context, idType, id, msgType, content string, input *types.SendInput) (string, string, error) {
	log.Printf("[Channel] Attempting to send message. idType: %s, id: %s, msgType: %s, replyID: %s", idType, id, msgType, input.ReplyMessageID)
	msgID, chatID, err := c.rawSendWithRetry(ctx, idType, id, msgType, content, input.ReplyMessageID)
	if err == nil {
		log.Printf("[Channel] Message sent successfully. msgID: %s, chatID: %s", msgID, chatID)
		return msgID, chatID, nil
	}

	fce := types.ClassifyError(err)
	log.Printf("[Channel] Send failed. Error: %v, ClassifiedCode: %v", err, fce.Code)

	// Fallback 1: Reply target gone -> remove ReplyMessageID and resend
	if fce.Code == types.ErrCodeTargetRevoked && input.ReplyMessageID != "" {
		log.Printf("[Channel] Fallback triggered: Reply target revoked. Retrying as new message.")
		input.ReplyMessageID = "" // downgrade to new message
		return c.sendOneWithFallback(ctx, idType, id, msgType, content, input)
	}

	// Fallback 2: Format error -> downgrade to text
	if fce.Code == types.ErrCodeFormatError && msgType != "text" {
		log.Printf("[Channel] Fallback triggered: Format error. Downgrading to text message.")
		fallbackText := ""
		if input.Markdown != "" {
			fallbackText = input.Markdown
		} else if input.Text != "" {
			fallbackText = input.Text
		} else {
			log.Printf("[Channel] Fallback failed: No text fallback available.")
			return "", "", err
		}

		prefix := normalize.ComposeMentionsTextPrefix(input.Mentions)
		fullText := prefix + fallbackText

		contentMap := map[string]string{"text": fullText}
		b, _ := json.Marshal(contentMap)

		return c.rawSendWithRetry(ctx, idType, id, "text", string(b), input.ReplyMessageID)
	}

	log.Printf("[Channel] No fallback applicable. Returning error.")
	return "", "", err
}

func splitPlain(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var out []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += limit {
		end := i + limit
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func (c *channelImpl) rawSendWithRetry(ctx context.Context, idType, id, msgType, content, replyMessageID string) (string, string, error) {
	var op func(attempt int) (interface{}, error)

	if replyMessageID != "" {
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(replyMessageID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				MsgType(msgType).
				Content(content).
				Build()).
			Build()

		op = func(attempt int) (interface{}, error) {
			log.Printf("[Channel] Sending reply message. Attempt: %d, ReplyID: %s, MsgType: %s", attempt, replyMessageID, msgType)
			resp, err := c.client.Im.V1.Message.Reply(ctx, req)
			if err != nil {
				log.Printf("[Channel] Reply message HTTP error: %v", err)
				return nil, err
			}
			if !resp.Success() {
				log.Printf("[Channel] Reply message API error: Code=%d, Msg=%s", resp.Code, resp.Msg)
				apiErr := &larkcore.CodeError{Code: resp.Code, Msg: resp.Msg}
				return nil, apiErr
			}
			return resp, nil
		}
	} else {
		req := larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(idType).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(id).
				MsgType(msgType).
				Content(content).
				Build()).
			Build()

		op = func(attempt int) (interface{}, error) {
			log.Printf("[Channel] Sending create message. Attempt: %d, IDType: %s, ID: %s, MsgType: %s", attempt, idType, id, msgType)
			resp, err := c.client.Im.V1.Message.Create(ctx, req)
			if err != nil {
				log.Printf("[Channel] Create message HTTP error: %v", err)
				return nil, err
			}
			if !resp.Success() {
				log.Printf("[Channel] Create message API error: Code=%d, Msg=%s", resp.Code, resp.Msg)
				apiErr := &larkcore.CodeError{Code: resp.Code, Msg: resp.Msg}
				return nil, apiErr
			}
			return resp, nil
		}
	}

	res, err := outbound.Retry(ctx, op, &outbound.RetryOptions{
		MaxAttempts: c.config.Outbound.Retry.MaxAttempts,
		BaseDelay:   c.config.Outbound.Retry.BaseDelayMs,
	})
	if err != nil {
		return "", "", types.ClassifyError(err)
	}

	chatID := ""
	if replyMessageID != "" {
		resp := res.(*larkim.ReplyMessageResp)
		if resp.Data.ChatId != nil {
			chatID = *resp.Data.ChatId
		}
		return *resp.Data.MessageId, chatID, nil
	} else {
		resp := res.(*larkim.CreateMessageResp)
		if resp.Data.ChatId != nil {
			chatID = *resp.Data.ChatId
		}
		return *resp.Data.MessageId, chatID, nil
	}
}
