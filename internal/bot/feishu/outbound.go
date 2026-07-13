package feishu

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/bot"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// EditMessage 原地编辑一条已发送的消息（Im.Message.Patch，仅对 interactive
// card 消息有效）。bot 渲染器用它实现回合中的流式输出。
func (a *adapter) EditMessage(ctx context.Context, messageID string, msg bot.OutboundMessage) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return fmt.Errorf("feishu edit: empty message id")
	}
	content, err := buildMarkdownCard(msg.Text)
	if err != nil {
		return err
	}
	client, err := a.sdkClient()
	if err != nil {
		return err
	}
	return withTransientRetry(ctx, a.logger, "patch message", func(ctx context.Context) error {
		req := larkim.NewPatchMessageReqBuilder().
			MessageId(messageID).
			Body(larkim.NewPatchMessageReqBodyBuilder().Content(content).Build()).
			Build()
		resp, err := client.Im.Message.Patch(ctx, req)
		if err != nil {
			return err
		}
		if resp == nil {
			return fmt.Errorf("feishu patch error: empty response")
		}
		if !resp.Success() {
			return fmt.Errorf("feishu patch error: %s", feishuCodeError(resp.Code, resp.Msg))
		}
		return nil
	})
}
